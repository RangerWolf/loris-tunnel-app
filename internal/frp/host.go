package frp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/forward"
)

type Host struct {
	cmd                                      *exec.Cmd
	done                                     chan struct{}
	events                                   chan forward.RuntimeEvent
	stopOnce                                 sync.Once
	mu                                       sync.Mutex
	err                                      error
	stopped                                  bool
	originHost                               string
	originPort                               int
	originState                              string
	address, endpoint, password, name, token string
}
type proxyStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Err    string `json:"err"`
}

func (h *Host) status(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", h.endpoint+"/api/status", nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth("loris", h.password)
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("FRP client status unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("FRP client status HTTP %d", resp.StatusCode)
	}
	var result map[string][]proxyStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return false, fmt.Errorf("invalid FRP client status")
	}
	for _, items := range result {
		for _, item := range items {
			if item.Name == h.name {
				if item.Err != "" {
					message := item.Err
					message = strings.ReplaceAll(message, h.password, "[redacted]")
					if h.token != "" {
						message = strings.ReplaceAll(message, h.token, "[redacted]")
					}
					if len(message) > 500 {
						message = message[:500]
					}
					return false, fmt.Errorf("FRP proxy: %s", message)
				}
				if item.Status == "running" {
					return true, nil
				}
			}
		}
	}
	return false, fmt.Errorf("FRP proxy is reconnecting; check server address and authentication")
}
func (c *Client) Start(ctx context.Context, s Server, name, kind, host string, port, remote int, domain string) (*Host, error) {
	path, err := c.resolve()
	if err != nil {
		return nil, fmt.Errorf("frpc is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	adminPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	password := randomID()
	data, err := configuration(s, name, kind, host, port, remote, domain, adminPort, password)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(filepath.Dir(c.Store.Path), "frp")
	if err = os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(dir, "frpc.toml")
	if err = conf.WritePrivateAtomic(configPath, data); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	cmd := exec.Command(path, "-c", configPath)
	configureCommand(cmd)
	// Raw frpc output can include server-controlled text or credentials; use the status API instead.
	diagnostic := &failureHint{}
	cmd.Stdout = diagnostic
	cmd.Stderr = diagnostic
	fullName := name
	if s.User != "" {
		fullName = s.User + "." + name
	}
	h := &Host{originHost: host, originPort: port, cmd: cmd, done: make(chan struct{}), events: make(chan forward.RuntimeEvent, 8), name: fullName, token: s.Token, password: password, endpoint: "http://127.0.0.1:" + strconv.Itoa(adminPort), address: Address(s, kind, remote, domain)}
	if err = ctx.Err(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("start frpc: %w", err)
	}
	go func() {
		err := cmd.Wait()
		h.mu.Lock()
		if !h.stopped {
			if err == nil {
				h.err = fmt.Errorf("FRP client exited")
			} else {
				h.err = fmt.Errorf("FRP client exited unexpectedly; check client version and server configuration")
			}
		}
		h.mu.Unlock()
		os.RemoveAll(dir)
		close(h.done)
	}()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var last error
	for {
		select {
		case <-ctx.Done():
			h.Stop()
			if hint := diagnostic.message(); hint != "" {
				return nil, fmt.Errorf("%s", hint)
			}
			if last != nil {
				return nil, fmt.Errorf("FRP start failed: %w", last)
			}
			return nil, ctx.Err()
		case <-h.done:
			if hint := diagnostic.message(); hint != "" {
				return nil, fmt.Errorf("%s", hint)
			}
			return nil, h.Err()
		case <-tick.C:
			ready, err := h.status(ctx)
			last = err
			if ready {
				h.checkOrigin()
				go h.monitor()
				return h, nil
			}
			if err != nil && strings.HasPrefix(err.Error(), "FRP proxy:") {
				h.Stop()
				return nil, err
			}
		}
	}
}
func (h *Host) monitor() {
	defer close(h.events)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	wasReady := true
	for {
		select {
		case <-h.done:
			return
		case <-tick.C:
			h.checkOrigin()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			ready, err := h.status(ctx)
			cancel()
			if ready != wasReady {
				event := forward.RuntimeEvent{Type: forward.RuntimeEventDisconnected, Err: err}
				if ready {
					event = forward.RuntimeEvent{Type: forward.RuntimeEventReconnected}
				}
				select {
				case h.events <- event:
				case <-h.done:
					return
				}
				wasReady = ready
			}
		}
	}
}
func (h *Host) checkOrigin() {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(h.originHost, strconv.Itoa(h.originPort)), time.Second)
	state := "unreachable"
	if err == nil {
		conn.Close()
		state = "reachable"
	}
	h.mu.Lock()
	h.originState = state
	h.mu.Unlock()
}
func (h *Host) OriginState() string { h.mu.Lock(); defer h.mu.Unlock(); return h.originState }
func (h *Host) Stop() error {
	h.stopOnce.Do(func() {
		h.mu.Lock()
		h.stopped = true
		h.mu.Unlock()
		select {
		case <-h.done:
			return
		default:
		}
		// Ask frpc to close its proxies/control connection before terminating it.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var err error
		if len(h.cmd.Args) >= 3 && h.cmd.Args[1] == "-c" {
			stop := exec.CommandContext(ctx, h.cmd.Path, "stop", "-c", h.cmd.Args[2], "--api-timeout", "2s")
			configureCommand(stop)
			err = stop.Run()
		} else {
			err = fmt.Errorf("no FRP config")
		}
		cancel()
		if err != nil {
			requestStop(h.cmd)
		}
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-h.done:
		case <-timer.C:
			forceStop(h.cmd)
		}
	})
	<-h.done
	return nil
}
func (h *Host) Done() <-chan struct{}               { return h.done }
func (h *Host) Events() <-chan forward.RuntimeEvent { return h.events }
func (h *Host) Err() error                          { h.mu.Lock(); defer h.mu.Unlock(); return h.err }
func (h *Host) PublicURL() string                   { return h.address }
