package devtunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"loris-tunnel/internal/forward"
)

const (
	readyTimeout   = 45 * time.Second
	retryWindow    = 2 * time.Hour
	initialBackoff = 2 * time.Second
	maxBackoff     = 60 * time.Second
)

type ServiceTarget struct {
	Host      string
	Port      int
	LocalPort int
}

// Host is one devtunnel host process plus the outer restart loop.
type Host struct {
	client     *Client
	tunnelID   string
	targetHost string
	targetPort int
	targets    []ServiceTarget

	mu           sync.Mutex
	cmd          *exec.Cmd
	url          string
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

func (c *Client) NewHost(tunnelID, targetHost string, targetPort int) *Host {
	return &Host{
		client:       c,
		tunnelID:     tunnelID,
		targetHost:   targetHost,
		targetPort:   targetPort,
		ready:        make(chan struct{}),
		done:         make(chan struct{}),
		events:       make(chan forward.RuntimeEvent, 16),
		readyTimeout: readyTimeout,
		retryDelay:   initialBackoff,
	}
}

// NewServiceHost hosts only the active service ports of a shared Tunnel ID.
func (c *Client) NewServiceHost(id string, targets []ServiceTarget) *Host {
	h := c.NewHost(id, "127.0.0.1", 0)
	h.targets = append([]ServiceTarget{}, targets...)
	return h
}

// Start blocks until the CLI prints the ready line or the process fails.
func (h *Host) Start() error {
	if !validTunnelID(h.tunnelID) {
		return fmt.Errorf("invalid dev tunnel id")
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
		return errors.New("dev tunnel host exited before it was ready")
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
		return errors.New("timed out stopping dev tunnel CLI")
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

func (h *Host) URL() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.url
}

func (h *Host) supervise(ctx context.Context) {
	defer h.finish()
	owner, err := h.client.claimHost(h.tunnelID)
	if err != nil {
		h.setErr(err)
		return
	}
	defer owner.close()
	if ctx.Err() != nil {
		return
	}
	targets := h.targets
	if len(targets) == 0 {
		targets = []ServiceTarget{{Host: h.targetHost, Port: h.targetPort}}
	}
	for _, target := range targets {
		localPort := target.LocalPort
		if localPort == 0 {
			localPort = target.Port
		}
		if directLocalTarget(target.Host) && localPort == target.Port {
			continue
		}
		closeBridge, err := startMappedTargetBridge(ctx, target.Host, localPort, target.Port)
		if err != nil {
			h.setErr(err)
			return
		}
		defer closeBridge()
	}
	var deadline time.Time
	delay := h.retryDelay
	connectedBefore := false
	hostConfigured := false
	for {
		var ready atomic.Bool
		if hostConfigured {
			detail := h.client.GetResource(ctx, h.tunnelID)
			if detail.State != ResourceReady || !configuredPortsAreActive(detail.Resource, h.targets) {
				h.setErr(fmt.Errorf("cannot safely host existing tunnel: configured ports do not match active services"))
				return
			}
		}
		err := h.runOnce(ctx, &ready, owner, hostConfigured)
		if !hostConfigured && !ready.Load() && err != nil && strings.Contains(err.Error(), "Batch update of ports is not supported") {
			hostConfigured = true
			continue
		}
		if ready.Load() {
			connectedBefore = true
			deadline = time.Now().Add(retryWindow)
			delay = h.retryDelay
		}
		h.mu.Lock()
		h.url = ""
		h.mu.Unlock()
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

func (h *Host) runOnce(ctx context.Context, ready *atomic.Bool, owner *hostOwner, configured bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	path, _, err := h.client.resolve()
	if err != nil {
		return fmt.Errorf("dev tunnel cli is not installed")
	}
	args := []string{"host", h.tunnelID}
	if !configured {
		for _, target := range h.targets {
			args = append(args, "-p", strconv.Itoa(target.Port))
		}
	}
	cmd := exec.Command(path, args...)
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
	// ponytail: on Unix, a crash before this record is saved can leave an untracked CLI;
	// a parent watchdog would be needed to close that startup window.
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
	attemptReady := make(chan struct{}, 1)
	readDone := make(chan string, 1)
	go func() { readDone <- h.readOutput(stdout, ready, attemptReady) }()
	hostExitError := func(err error) error {
		var output string
		select {
		case output = <-readDone:
		case <-time.After(time.Second):
			_ = stdout.Close()
			output = <-readDone
		}
		output = strings.TrimSpace(output)
		if err == nil {
			err = errors.New("dev tunnel host exited")
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
			return hostExitError(err)
		case <-timer.C:
			killProcessTree(cmd)
			<-waitDone
			return hostExitError(errors.New("dev tunnel host did not become ready in time"))
		case <-ctx.Done():
			killProcessTree(cmd)
			<-waitDone
			return hostExitError(ctx.Err())
		}
	}

	select {
	case err := <-waitDone:
		return hostExitError(err)
	case <-ctx.Done():
		killProcessTree(cmd)
		<-waitDone
		return hostExitError(ctx.Err())
	}
}

func (h *Host) readOutput(stdout io.Reader, ready *atomic.Bool, attemptReady chan<- struct{}) string {
	var output, currentURL string
	var currentURLs []string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		output += line + "\n"
		if len(output) > 1000 {
			output = output[len(output)-1000:]
		}
		url, isReady, auth := ParseHostLine(line)
		if auth {
			h.setErr(errors.New("dev tunnel login expired"))
			h.mu.Lock()
			cmd := h.cmd
			h.mu.Unlock()
			killProcessTree(cmd)
			return output
		}
		if strings.HasPrefix(url, "https://") {
			currentURLs = mergeURLs(currentURLs, ParseHostURLs(line))
			currentURL = strings.Join(preferURLList(currentURLs), ", ")
		}
		if isReady && currentURL != "" && ready.CompareAndSwap(false, true) {
			h.mu.Lock()
			h.url = currentURL
			h.mu.Unlock()
			first := false
			h.readyOnce.Do(func() { first = true; close(h.ready) })
			if !first {
				h.emitEvent(forward.RuntimeEvent{Type: forward.RuntimeEventReconnected})
			}
			attemptReady <- struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		h.setErr(fmt.Errorf("read CLI output: %w", err))
		h.mu.Lock()
		cmd := h.cmd
		h.mu.Unlock()
		killProcessTree(cmd)
	}
	return output
}

func (h *Host) finish() {
	h.doneOnce.Do(func() { close(h.events); close(h.done) })
}

func (h *Host) setErr(err error) {
	if err == nil {
		err = errors.New("dev tunnel host exited before it was ready")
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
		strings.Contains(text, "read cli output"),
		strings.Contains(text, "access is denied"),
		strings.Contains(text, "already connected"),
		strings.Contains(text, "already hosted"),
		strings.Contains(text, "conflict"),
		strings.Contains(text, "login"),
		strings.Contains(text, "auth"),
		strings.Contains(text, "not signed"),
		strings.Contains(text, "expired"),
		strings.Contains(text, "not found"),
		strings.Contains(text, "invalid"),
		strings.Contains(text, "not installed"),
		strings.Contains(text, "permission"):
		return false
	default:
		return true
	}
}

// Hosting an existing ID exposes all configured ports, so never include paused services.
func configuredPortsAreActive(resource Resource, targets []ServiceTarget) bool {
	if !resource.PortsKnown || len(resource.Ports) == 0 || len(targets) == 0 {
		return false
	}
	active := make(map[int]bool, len(targets))
	for _, target := range targets {
		active[target.Port] = true
	}
	if len(active) != len(resource.Ports) {
		return false
	}
	for _, port := range resource.Ports {
		if !active[port.Number] {
			return false
		}
	}
	return true
}
