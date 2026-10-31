package cloudflared

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"loris-tunnel/internal/forward"
)

var errQUICUnavailable = errors.New("QUIC handshake timed out")

const (
	readyTimeout   = 45 * time.Second
	retryWindow    = 2 * time.Hour
	initialBackoff = 2 * time.Second
	maxBackoff     = 60 * time.Second
)

// Host is one cloudflared process plus the outer restart loop.
// cloudflared reconnects inside the process; startup QUIC timeouts trigger HTTP/2 fallback.
type Host struct {
	client     *Client
	tunnelID   string
	configPath string
	hostname   string

	mu           sync.Mutex
	cmd          *exec.Cmd
	err          error
	ready        chan struct{}
	done         chan struct{}
	readyOnce    sync.Once
	doneOnce     sync.Once
	cancel       context.CancelFunc
	stopping     bool
	started      bool
	events       chan forward.RuntimeEvent
	readyTimeout time.Duration
	retryDelay   time.Duration
}

func (c *Client) NewHost(tunnelID, configPath, hostname string) *Host {
	return &Host{
		client:       c,
		tunnelID:     tunnelID,
		configPath:   configPath,
		hostname:     hostname,
		ready:        make(chan struct{}),
		done:         make(chan struct{}),
		events:       make(chan forward.RuntimeEvent, 16),
		readyTimeout: readyTimeout,
		retryDelay:   initialBackoff,
	}
}

// Start blocks until cloudflared registers a connection or the process fails.
func (h *Host) Start() error {
	if !ValidTunnelID(h.tunnelID) {
		return fmt.Errorf("invalid cloudflare tunnel id")
	}
	h.mu.Lock()
	if h.stopping {
		h.mu.Unlock()
		return context.Canceled
	}
	if !h.started {
		ctx, cancel := context.WithCancel(context.Background())
		h.started = true
		h.cancel = cancel
		go h.supervise(ctx)
	}
	h.mu.Unlock()
	select {
	case <-h.ready:
		select {
		case <-h.done:
			if err := h.Err(); err != nil {
				return err
			}
			return context.Canceled
		default:
		}
		if h.isStopping() {
			return context.Canceled
		}
		return nil
	case <-h.done:
		if err := h.Err(); err != nil {
			return err
		}
		return errors.New("cloudflared exited before it was ready")
	}
}

func (h *Host) Stop() error {
	h.mu.Lock()
	h.stopping = true
	started := h.started
	cancel := h.cancel
	h.mu.Unlock()
	if !started {
		h.finish()
		return nil
	}
	cancel()
	select {
	case <-h.done:
		return nil
	case <-time.After(8 * time.Second):
		return errors.New("timed out stopping cloudflared")
	}
}

func (h *Host) Events() <-chan forward.RuntimeEvent { return h.events }

func (h *Host) emitEvent(event forward.RuntimeEvent) {
	select {
	case h.events <- event:
	default:
	}
}

func (h *Host) Done() <-chan struct{} { return h.done }

func (h *Host) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

func (h *Host) PublicURL() string { return PublicURL(h.hostname) }

func (h *Host) supervise(ctx context.Context) {
	defer h.finish()
	owner, err := h.client.claimHost(h.tunnelID)
	if err != nil {
		h.setErr(err)
		return
	}
	defer owner.close()
	var deadline time.Time
	delay := h.retryDelay
	connectedBefore := false
	protocol := "auto"
	for {
		var ready atomic.Bool
		err := h.runOnce(ctx, &ready, owner, protocol)
		if !ready.Load() && protocol == "auto" && errors.Is(err, errQUICUnavailable) && ctx.Err() == nil && !h.isStopping() {
			protocol = "http2"
			slog.Warn("cloudflared switching transport", "tunnel_id", h.tunnelID, "from", "auto", "to", protocol, "reason", "QUIC handshake timed out; retrying over TCP")
			continue
		}
		if ready.Load() {
			connectedBefore = true
			deadline = time.Now().Add(retryWindow)
			delay = h.retryDelay
		}
		if h.isStopping() || ctx.Err() != nil {
			return
		}
		if !connectedBefore || !retryableHostError(err) || time.Now().After(deadline) {
			h.setErr(err)
			return
		}
		if ready.Load() {
			h.emitEvent(forward.RuntimeEvent{Type: forward.RuntimeEventDisconnected, Err: err})
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < maxBackoff {
			delay *= 2
			if delay > maxBackoff {
				delay = maxBackoff
			}
		}
	}
}

func (h *Host) runOnce(ctx context.Context, ready *atomic.Bool, owner *hostOwner, protocol string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	path, _, err := h.client.resolve()
	if err != nil {
		return ErrNotInstalled
	}
	cmd := exec.Command(path, "tunnel", "--config", h.configPath, "run", "--protocol", protocol, h.tunnelID)
	slog.Info("cloudflared starting transport", "tunnel_id", h.tunnelID, "protocol", protocol)
	configureCommand(cmd)
	stdout, output, err := os.Pipe()
	if err != nil {
		return err
	}
	defer stdout.Close()
	cmd.Stdout = output
	cmd.Stderr = output
	if ctx.Err() != nil {
		output.Close()
		return ctx.Err()
	}
	if err := cmd.Start(); err != nil {
		output.Close()
		return err
	}
	output.Close()
	closeProcess, err := containHostProcess(cmd)
	if err != nil {
		killProcessTree(cmd)
		_ = cmd.Wait()
		return fmt.Errorf("contain CLI process: %w", err)
	}
	defer closeProcess()
	if err := owner.track(cmd.Process.Pid); err != nil {
		killProcessTree(cmd)
		_ = cmd.Wait()
		return fmt.Errorf("record CLI process: %w", err)
	}
	defer os.Remove(owner.record)
	h.mu.Lock()
	h.cmd = cmd
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.cmd = nil; h.mu.Unlock() }()
	var quicFailed chan struct{}
	if protocol == "auto" {
		quicFailed = make(chan struct{}, 1)
	}
	attemptReady := make(chan struct{}, 1)
	readDone := make(chan string, 1)
	go func() { readDone <- h.readOutput(stdout, ready, attemptReady, quicFailed) }()
	exitError := func(err error) error {
		var output string
		select {
		case output = <-readDone:
		case <-time.After(time.Second):
			_ = stdout.Close()
			output = <-readDone
		}
		output = strings.TrimSpace(output)
		if err == nil {
			err = errors.New("cloudflared exited")
		}
		if output != "" {
			return fmt.Errorf("%w: %s", err, output)
		}
		return err
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	if !ready.Load() {
		timer := time.NewTimer(h.readyTimeout)
		defer timer.Stop()
		select {
		case <-attemptReady:
		case err := <-waitDone:
			return exitError(err)
		case <-quicFailed:
			if !ready.Load() && protocol == "auto" {
				killProcessTree(cmd)
				<-waitDone
				return exitError(errQUICUnavailable)
			}
		case <-timer.C:
			killProcessTree(cmd)
			<-waitDone
			return exitError(errors.New("cloudflared did not become ready in time"))
		case <-ctx.Done():
			killProcessTree(cmd)
			<-waitDone
			return exitError(ctx.Err())
		}
	}
	select {
	case err := <-waitDone:
		return exitError(err)
	case <-ctx.Done():
		killProcessTree(cmd)
		<-waitDone
		return exitError(ctx.Err())
	}
}

func (h *Host) readOutput(stdout io.Reader, ready *atomic.Bool, attemptReady chan<- struct{}, quicFailed chan<- struct{}) string {
	var output string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		lower := strings.ToLower(line)
		if !ready.Load() && strings.Contains(lower, "quic") && strings.Contains(lower, "timeout") && (strings.Contains(lower, "failed to dial") || strings.Contains(lower, "handshake")) {
			select {
			case quicFailed <- struct{}{}:
			default:
			}
		}
		output += line + "\n"
		if len(output) > 1000 {
			output = output[len(output)-1000:]
		}
		if ReadyLine(line) && ready.CompareAndSwap(false, true) {
			slog.Info("cloudflared connection ready", "tunnel_id", h.tunnelID)
			first := false
			h.readyOnce.Do(func() { first = true; close(h.ready) })
			if !first {
				h.emitEvent(forward.RuntimeEvent{Type: forward.RuntimeEventReconnected})
			}
			select {
			case attemptReady <- struct{}{}:
			default:
			}
		}
	}
	return output
}

func (h *Host) finish() {
	if h.configPath != "" {
		_ = os.Remove(h.configPath)
	}
	h.doneOnce.Do(func() { close(h.events); close(h.done) })
}

func (h *Host) setErr(err error) {
	if err == nil {
		err = errors.New("cloudflared exited before it was ready")
	}
	h.mu.Lock()
	if h.err == nil && !h.stopping {
		h.err = err
	}
	h.mu.Unlock()
}

func (h *Host) isStopping() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopping
}

func retryableHostError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "contain cli process"),
		strings.Contains(text, "record cli process"),
		strings.Contains(text, "credentials are missing"),
		strings.Contains(text, "authentication is required"),
		strings.Contains(text, "dns record already exists"),
		strings.Contains(text, "not found"),
		strings.Contains(text, "invalid"),
		strings.Contains(text, "not installed"),
		strings.Contains(text, "permission"):
		return false
	default:
		return true
	}
}
