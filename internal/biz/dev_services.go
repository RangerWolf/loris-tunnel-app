package biz

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

type devServiceHost interface {
	tunnelRuntime
	Start() error
	URL() string
}

type devServiceSession struct {
	key        string
	host       devServiceHost
	generation uint64
	members    map[int]*devServiceRuntime
}

// One service owns its row lifecycle; the session owns the single CLI process.
type devServiceRuntime struct {
	biz       *TunnelBiz
	key       string
	id        int
	target    devtunnel.ServiceTarget
	done      chan struct{}
	events    chan forward.RuntimeEvent
	mu        sync.Mutex
	url       string
	err       error
	reconnect *model.ReconnectInfo
	closed    bool // guarded by devSessionMu
}

func (r *devServiceRuntime) Done() <-chan struct{}               { return r.done }
func (r *devServiceRuntime) Events() <-chan forward.RuntimeEvent { return r.events }
func (r *devServiceRuntime) Err() error                          { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *devServiceRuntime) PublicURL() string                   { r.mu.Lock(); defer r.mu.Unlock(); return r.url }
func (r *devServiceRuntime) ReconnectInfo() *model.ReconnectInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reconnect == nil {
		return nil
	}
	info := *r.reconnect
	info.HostIDs = append([]string(nil), info.HostIDs...)
	return &info
}

func (r *devServiceRuntime) Stop() error { return r.biz.stopDevService(r) }
func (r *devServiceRuntime) finish(err error) {
	if r.closed {
		return
	}
	r.closed = true
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
	close(r.events)
	close(r.done)
}
func (r *devServiceRuntime) emit(event forward.RuntimeEvent) {
	select {
	case r.events <- event:
	default:
	}
}

func (b *TunnelBiz) startDevService(ctx context.Context, r *devServiceRuntime) error {
	unlock := b.lockResource(resourceLockKey("devtunnel", r.key))
	defer unlock()
	return b.startDevServiceLocked(ctx, r)
}

func (b *TunnelBiz) startDevServiceLocked(ctx context.Context, r *devServiceRuntime) error {
	b.devSessionMu.Lock()
	defer b.devSessionMu.Unlock()
	if r.closed || ctx.Err() != nil {
		r.finish(context.Canceled)
		return context.Canceled
	}
	if b.devSessions == nil {
		b.devSessions = make(map[string]*devServiceSession)
	}
	session := b.devSessions[r.key]
	if session == nil {
		session = &devServiceSession{key: r.key, members: make(map[int]*devServiceRuntime)}
		b.devSessions[r.key] = session
	}
	if len(session.members) > 0 {
		err := fmt.Errorf("Tunnel ID %s already hosts a service; use a separate Tunnel ID", r.key)
		r.finish(err)
		return err
	}
	session.members[r.id] = r
	if err := b.restartDevSession(ctx, session); err != nil {
		delete(session.members, r.id)
		r.finish(err)
		// Restore the previously running services if adding a new service failed.
		restoreCtx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		if len(session.members) > 0 {
			if restoreErr := b.restartDevSession(restoreCtx, session); restoreErr != nil {
				b.failDevSession(session, restoreErr)
			}
		} else {
			delete(b.devSessions, r.key)
		}
		return err
	}
	return nil
}

func (b *TunnelBiz) stopDevService(r *devServiceRuntime) error {
	b.devSessionMu.Lock()
	defer b.devSessionMu.Unlock()
	if r.closed {
		return nil
	}
	session := b.devSessions[r.key]
	if session == nil || session.members[r.id] != r {
		r.finish(nil)
		return nil
	}
	delete(session.members, r.id)
	r.finish(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	if err := b.restartDevSession(ctx, session); err != nil {
		// Never restore a deliberately stopped port after a failed reconfiguration.
		b.failDevSession(session, err)
		return err
	}
	if len(session.members) == 0 {
		delete(b.devSessions, r.key)
	}
	return nil
}

// CLI port selection changes require a brief reconnect; row state and ownership stay separate.
func (b *TunnelBiz) restartDevSession(ctx context.Context, session *devServiceSession) error {
	session.generation++
	if session.host != nil {
		if err := session.host.Stop(); err != nil {
			return err
		}
		session.host = nil
	}
	if len(session.members) == 0 {
		return nil
	}
	targets := make([]devtunnel.ServiceTarget, 0, len(session.members))
	for _, r := range session.members {
		targets = append(targets, r.target)
		if r.PublicURL() != "" {
			r.emit(forward.RuntimeEvent{Type: forward.RuntimeEventDisconnected, Err: fmt.Errorf("updating shared Tunnel ID services")})
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Port < targets[j].Port })
	var host devServiceHost
	if b.newDevServiceHost != nil {
		host = b.newDevServiceHost(session.key, targets)
	} else {
		host = b.dev.NewServiceHost(session.key, targets)
	}
	session.host = host
	stopOnCancel := context.AfterFunc(ctx, func() { _ = host.Stop() })
	err := host.Start()
	stopOnCancel()
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		for _, value := range strings.Split(host.URL(), ",") {
			matched := false
			for _, target := range targets {
				if len(devtunnel.URLsForPort([]string{value}, target.Port)) > 0 {
					matched = true
					break
				}
			}
			if !matched {
				err = fmt.Errorf("dev tunnel CLI published a port outside the active service set")
				break
			}
		}
	}
	if err == nil {
		for _, r := range session.members {
			urls := devtunnel.URLsForPort(strings.Split(host.URL(), ","), r.target.Port)
			if len(urls) == 0 {
				err = fmt.Errorf("dev tunnel CLI did not publish service port %d", r.target.Port)
				break
			}
			r.mu.Lock()
			r.url = strings.Join(urls, ", ")
			r.mu.Unlock()
		}
	}
	if err != nil {
		_ = host.Stop()
		session.host = nil
		return err
	}
	for _, r := range session.members {
		r.emit(forward.RuntimeEvent{Type: forward.RuntimeEventReconnected})
	}
	generation := session.generation
	go b.watchDevSession(session, host, generation)
	return nil
}

func (b *TunnelBiz) failDevSession(session *devServiceSession, err error) {
	session.generation++
	if session.host != nil {
		_ = session.host.Stop()
		session.host = nil
	}
	for _, r := range session.members {
		r.finish(err)
	}
	delete(b.devSessions, session.key)
}

func (b *TunnelBiz) watchDevSession(session *devServiceSession, host devServiceHost, generation uint64) {
	events := host.Events()
	for {
		select {
		case <-host.Done():
			b.devSessionMu.Lock()
			if session.generation == generation {
				b.failDevSession(session, host.Err())
			}
			b.devSessionMu.Unlock()
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			b.devSessionMu.Lock()
			if session.generation != generation {
				b.devSessionMu.Unlock()
				return
			}
			if event.Type == forward.RuntimeEventReconnected {
				published := event.PublicURL
				if published == "" {
					published = host.URL()
				}
				for _, value := range strings.Split(published, ",") {
					matched := false
					for _, r := range session.members {
						if len(devtunnel.URLsForPort([]string{value}, r.target.Port)) > 0 {
							matched = true
							break
						}
					}
					if !matched {
						b.failDevSession(session, fmt.Errorf("dev tunnel CLI republished a port outside the active service set"))
						b.devSessionMu.Unlock()
						return
					}
				}
				for _, r := range session.members {
					urls := devtunnel.URLsForPort(strings.Split(published, ","), r.target.Port)
					if len(urls) == 0 {
						b.failDevSession(session, fmt.Errorf("dev tunnel CLI did not republish service port %d", r.target.Port))
						b.devSessionMu.Unlock()
						return
					}
					r.mu.Lock()
					r.url = strings.Join(urls, ", ")
					r.mu.Unlock()
				}
			}
			for _, r := range session.members {
				r.mu.Lock()
				r.reconnect = event.Reconnect
				r.mu.Unlock()
				r.emit(event)
			}
			b.devSessionMu.Unlock()
		}
	}
}

// Always derive the published port from the current service, never a stale binding.
func (b *TunnelBiz) allocateDevTunnelPort(host string, localPort int) (int, error) {
	if localPort < 1 || localPort > 65535 {
		return 0, fmt.Errorf("invalid local service port")
	}
	if strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1" {
		return localPort, nil
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(localPort)))
	if err != nil {
		listener, err = net.Listen("tcp4", "127.0.0.1:0")
	}
	if err != nil {
		return 0, fmt.Errorf("allocate public service port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port, nil
}
