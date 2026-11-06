package devtunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

const (
	readyTimeout   = 45 * time.Second
	initialBackoff = time.Second
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
	stableWindow time.Duration
	jitter       func(time.Duration) time.Duration
	readyAt      time.Time
	recoveryAt   time.Time
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
		stableWindow: time.Minute,
		jitter:       func(d time.Duration) time.Duration { return time.Duration(float64(d) * (0.8 + rand.Float64()*0.4)) },
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
	failures := 0
	var lastDiagnostic, lastLog time.Time
	var hostIDs []string
	connectedBefore := false
	hostConfigured := false
	for {
		var ready atomic.Bool
		var attemptErr error
		if hostConfigured {
			detail := h.client.GetResource(ctx, h.tunnelID)
			if detail.State != ResourceReady {
				attemptErr = fmt.Errorf("cannot inspect existing tunnel ports (%s): %s", detail.State, hostDiagnosticText(detail.Detail))
				if !connectedBefore || detail.State != ResourceNetwork {
					h.setErr(attemptErr)
					return
				}
			}
			if attemptErr == nil && !configuredPortsAreActive(detail.Resource, h.targets) {
				var configured, active, extra []int
				wanted := map[int]bool{}
				for _, target := range h.targets {
					active = append(active, target.Port)
					wanted[target.Port] = true
				}
				for _, port := range detail.Resource.Ports {
					configured = append(configured, port.Number)
					if !wanted[port.Number] {
						extra = append(extra, port.Number)
					}
				}
				h.setErr(fmt.Errorf("cannot safely host existing tunnel: configured ports do not match active services (remote=%v, active=%v, extra=%v, portsKnown=%t)", configured, active, extra, detail.Resource.PortsKnown))
				return
			}
		}
		err := attemptErr
		if err == nil {
			err = h.runOnce(ctx, &ready, owner, hostConfigured)
		}
		if !hostConfigured && !ready.Load() && err != nil && strings.Contains(err.Error(), "Batch update of ports is not supported") {
			hostConfigured = true
			continue
		}

		if ready.Load() {
			connectedBefore = true
			h.mu.Lock()
			stable := time.Since(h.readyAt) >= h.stableWindow
			h.mu.Unlock()
			if stable {
				failures = 0
				hostIDs = nil
			}
		}
		h.mu.Lock()
		h.url = ""
		h.mu.Unlock()
		if h.isStopping() || ctx.Err() != nil {
			return
		}
		if !connectedBefore || !retryableHostError(err) {
			h.setErr(err)
			return
		}
		h.mu.Lock()
		if h.recoveryAt.IsZero() {
			h.recoveryAt = time.Now()
		}
		h.mu.Unlock()
		failures++
		delay := h.jitter(reconnectDelay(h.retryDelay, failures))
		next := time.Now().Add(delay)
		kind := hostFailureKind(err)
		if kind == "host_conflict" && (lastDiagnostic.IsZero() || time.Since(lastDiagnostic) >= time.Minute) {
			lastDiagnostic = time.Now()
			probeCtx, cancel := context.WithTimeout(ctx, time.Second)
			hostIDs = h.client.observedHostIDs(probeCtx, h.tunnelID)
			cancel()
		}
		if ctx.Err() != nil {
			return
		}
		diagnostic := &model.ReconnectInfo{Kind: kind, Attempt: failures, DelaySeconds: delay.Seconds(), HostIDs: append([]string(nil), hostIDs...), Detail: clip(redactSecrets(err.Error()), 300)}
		h.emitEvent(forward.RuntimeEvent{Type: forward.RuntimeEventDisconnected, Err: err, Reconnect: diagnostic})
		if failures == 1 || time.Since(lastLog) >= time.Minute {
			slog.Warn("dev tunnel reconnect scheduled", "tunnel_id", h.tunnelID, "kind", kind, "attempt", failures, "delay", delay, "detail", diagnostic.Detail)
			lastLog = time.Now()
		}
		timer := time.NewTimer(max(time.Until(next), 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
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
	h.client.mu.Lock()
	version := h.client.version
	h.client.mu.Unlock()
	slog.Info("dev tunnel host process started", "tunnel_id", h.tunnelID, "pid", cmd.Process.Pid, "cli_version", version)
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
		// Reap any descendants left behind by an exited CLI before the next attempt.
		killProcessTree(cmd)
		var output string
		select {
		case output = <-readDone:
		case <-time.After(time.Second):
			_ = stdout.Close()
			output = <-readDone
		}
		kind := hostFailureKind(errors.New(output))
		output = strings.TrimSpace(hostDiagnosticText(output))
		if kind == "auth" {
			output = "dev tunnel login expired: " + output
		}
		if err == nil {
			err = errors.New("dev tunnel host exited")
		}
		if output != "" {
			return &hostAttemptError{kind: kind, err: fmt.Errorf("%w: %s", err, output)}
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
	var attemptReason string
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
		if auth || hostDisconnectLine(line) {
			if auth {
				attemptReason = "dev tunnel login expired"
			} else {
				attemptReason = "dev tunnel host disconnected: TooManyConnections"
			}
			h.mu.Lock()
			cmd := h.cmd
			h.mu.Unlock()
			killProcessTree(cmd)
			return attemptReason + "\n" + output
		}
		if strings.HasPrefix(url, "https://") {
			currentURLs = mergeURLs(currentURLs, ParseHostURLs(line))
			currentURL = strings.Join(preferURLList(currentURLs), ", ")
		}
		if isReady && currentURL != "" && ready.CompareAndSwap(false, true) {
			h.mu.Lock()
			h.url = currentURL
			h.readyAt = time.Now()
			recoveryAt := h.recoveryAt
			h.recoveryAt = time.Time{}
			h.mu.Unlock()
			first := false
			h.readyOnce.Do(func() { first = true; close(h.ready) })
			if !first {
				slog.Info("dev tunnel host recovered", "tunnel_id", h.tunnelID, "elapsed", time.Since(recoveryAt))
				h.emitEvent(forward.RuntimeEvent{Type: forward.RuntimeEventReconnected, PublicURL: currentURL})
			}
			attemptReady <- struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		attemptReason = fmt.Sprintf("read CLI output: %v", err)
		h.mu.Lock()
		cmd := h.cmd
		h.mu.Unlock()
		killProcessTree(cmd)
	}
	return attemptReason + "\n" + output
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
	return err != nil && hostFailureKind(err) != "fatal" && hostFailureKind(err) != "auth"
}

// Only explicit host/relay disconnection diagnostics trigger termination of a live CLI.
func hostDisconnectLine(line string) bool {
	text := strings.ToLower(line)
	return (strings.Contains(text, "toomanyconnections") || strings.Contains(text, "too many connections")) &&
		(strings.Contains(text, "disconnect") || strings.Contains(text, "relay") || strings.Contains(text, "ssh"))
}

func hostFailureKind(err error) string {
	if err == nil {
		return "fatal"
	}
	var attempt *hostAttemptError
	if errors.As(err, &attempt) {
		return attempt.kind
	}
	var diagnosticLines []string
	for _, line := range strings.Split(err.Error(), "\n") {
		// Successful readiness output can contain user-chosen IDs such as
		// "conflict" or "invalid"; those names are not failure diagnostics.
		if strings.Contains(line, "Connect via browser:") || strings.Contains(line, "Ready to accept connections for tunnel:") {
			continue
		}
		diagnosticLines = append(diagnosticLines, line)
	}
	text := strings.ToLower(strings.Join(diagnosticLines, "\n"))
	if strings.Contains(text, "toomanyconnections") || strings.Contains(text, "too many connections") {
		return "host_conflict"
	}
	for _, word := range []string{"login expired", "login token expired", "login required", "not logged in", "not signed", "authentication is required", "unauthorized", "token expired"} {
		if strings.Contains(text, word) {
			return "auth"
		}
	}
	for _, word := range []string{"contain cli process", "record cli process", "read cli output", "access is denied", "access denied", "already connected", "already hosted", "conflict", "not found", "invalid dev tunnel", "invalid tunnel", "invalid argument", "invalid configuration", "not installed", "permission", "forbidden"} {
		if strings.Contains(text, word) {
			return "fatal"
		}
	}
	return "transient"
}

func reconnectDelay(base time.Duration, failures int) time.Duration {
	// Scale from the production one-second base, allowing fast lifecycle tests.
	factors := [...]int{1, 2, 4, 8, 15, 30}
	i := min(max(failures-1, 0), len(factors)-1)
	return base * time.Duration(factors[i])
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
