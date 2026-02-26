package forward

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"loris-tunnel/internal/model"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

var ErrUnsupportedMode = errors.New("only local, remote and dynamic modes are supported")

const (
	initReconnectWait = 500 * time.Millisecond
	maxReconnectWait  = 1 * time.Minute
	reconnectTimeout  = 15 * time.Minute
)

var (
	sshAgentMu   sync.Mutex
	sshAgentInst agent.ExtendedAgent
	sshAgentSock string
	sshAgentConn net.Conn
)

type RuntimeEventType string

const (
	RuntimeEventDisconnected RuntimeEventType = "disconnected"
	RuntimeEventReconnected  RuntimeEventType = "reconnected"
)

type RuntimeEvent struct {
	Type RuntimeEventType
	Err  error
}

type LocalForward struct {
	tunnel  model.Tunnel
	jumpers []model.Jumper

	mu          sync.Mutex
	started     bool
	stopping    bool
	runErr      error
	client      *ssh.Client
	clientClose func()
	listener    net.Listener
	done        chan struct{}
	events      chan RuntimeEvent
	keepStop    chan struct{}
	lastLatency time.Duration
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

func NewLocalForward(tunnel model.Tunnel, jumpers []model.Jumper) *LocalForward {
	return &LocalForward{
		tunnel:  tunnel,
		jumpers: append([]model.Jumper{}, jumpers...),
	}
}

func (f *LocalForward) Start() error {
	mode := normalizeForwardMode(f.tunnel.Mode)
	if mode != "local" && mode != "remote" && mode != "dynamic" {
		return ErrUnsupportedMode
	}

	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return nil
	}
	f.started = true
	f.runErr = nil
	f.done = make(chan struct{})
	f.events = make(chan RuntimeEvent, 8)
	f.keepStop = make(chan struct{})
	f.mu.Unlock()

	slog.Info(
		"tunnel forward start",
		"tunnel_id", f.tunnel.ID,
		"name", f.tunnel.Name,
		"jumper_hops", len(f.jumpers),
		"keepalive_interval_ms", f.lastJumper().KeepAliveIntervalMs,
		"timeout_ms", f.lastJumper().TimeoutMs,
	)

	client, closeChain, err := dialSSHChain(f.jumpers)
	if err != nil {
		f.setRunErr(err)
		slog.Error("tunnel initial dial failed", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "err", err)
		return err
	}
	if mode == "local" {
		if err := probeRemoteDial(client, f.tunnel.RemoteHost, f.tunnel.RemotePort); err != nil {
			closeChain()
			f.setRunErr(err)
			slog.Error(
				"tunnel initial remote probe failed",
				"tunnel_id",
				f.tunnel.ID, "name", f.tunnel.Name, "err", err)
			return err
		}
	} else if mode == "dynamic" {
		if err := probeDynamicForwardCapability(client); err != nil {
			closeChain()
			f.setRunErr(err)
			slog.Error("tunnel dynamic probe failed", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "err", err)
			return err
		}
	}

	var ln net.Listener
	if mode == "remote" {
		ln, err = f.bindRemoteListener(client)
		if err != nil {
			closeChain()
			f.setRunErr(err)
			slog.Error("tunnel remote listen failed", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "err", err)
			return err
		}
	} else {
		localHost := strings.TrimSpace(f.tunnel.LocalHost)
		if localHost == "" {
			localHost = "127.0.0.1"
		}
		localAddr := net.JoinHostPort(localHost, strconv.Itoa(f.tunnel.LocalPort))
		ln, err = net.Listen("tcp", localAddr)
		if err != nil {
			closeChain()
			runErr := fmt.Errorf("listen %s failed: %w", localAddr, err)
			f.setRunErr(runErr)
			slog.Error("tunnel listen failed", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "addr", localAddr, "err", runErr)
			return runErr
		}
	}

	f.mu.Lock()
	f.client = client
	f.clientClose = closeChain
	f.listener = ln
	f.lastLatency = 0
	done := f.done
	f.mu.Unlock()

	if latency, latencyErr := TestJumperLatency(client); latencyErr == nil {
		f.setLastLatency(latency)
	}

	if mode == "remote" {
		go f.serveRemote(done)
	} else {
		go f.serveLocal(done)
	}
	go f.monitorClientLifecycle(client)
	return nil
}

func (f *LocalForward) Stop() error {
	f.stopOnce.Do(func() {
		slog.Info("tunnel forward stop requested", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name)
		f.mu.Lock()
		f.stopping = true
		ln := f.listener
		c := f.client
		closeClient := f.clientClose
		done := f.done
		keepStop := f.keepStop
		f.listener = nil
		f.client = nil
		f.clientClose = nil
		f.mu.Unlock()

		if keepStop != nil {
			close(keepStop)
		}
		if closeClient != nil {
			closeClient()
		} else if c != nil {
			_ = c.Close()
		}
		if ln != nil {
			_ = ln.Close()
		}
		if done != nil {
			<-done
		}
		f.wg.Wait()
	})
	return nil
}

func (f *LocalForward) Done() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.done
}

func (f *LocalForward) Events() <-chan RuntimeEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events
}

func (f *LocalForward) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runErr
}

func (f *LocalForward) LastLatency() (time.Duration, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastLatency <= 0 {
		return 0, false
	}
	return f.lastLatency, true
}

func (f *LocalForward) serveLocal(done chan struct{}) {
	defer close(done)

	for {
		f.mu.Lock()
		ln := f.listener
		f.mu.Unlock()
		if ln == nil {
			return
		}

		conn, err := ln.Accept()
		if err != nil {
			if f.isStopping() {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			if f.Err() == nil {
				f.setRunErr(fmt.Errorf("accept failed: %w", err))
			}
			return
		}

		f.wg.Add(1)
		go func(localConn net.Conn) {
			defer f.wg.Done()
			f.handleConn(localConn)
		}(conn)
	}
}

func (f *LocalForward) serveRemote(done chan struct{}) {
	defer close(done)

	for {
		if f.isStopping() {
			return
		}

		f.mu.Lock()
		ln := f.listener
		f.mu.Unlock()
		if ln == nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		conn, err := ln.Accept()
		if err != nil {
			if f.isStopping() {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}

		f.wg.Add(1)
		go func(remoteConn net.Conn) {
			defer f.wg.Done()
			f.handleConn(remoteConn)
		}(conn)
	}
}

func (f *LocalForward) handleConn(localConn net.Conn) {
	f.mu.Lock()
	client := f.client
	f.mu.Unlock()

	if client == nil {
		_ = localConn.Close()
		return
	}

	if normalizeForwardMode(f.tunnel.Mode) == "dynamic" {
		f.handleDynamicConn(localConn, client)
		return
	}
	if normalizeForwardMode(f.tunnel.Mode) == "remote" {
		f.handleRemoteConn(localConn)
		return
	}
	f.handleLocalConn(localConn, client)
}

func (f *LocalForward) handleLocalConn(localConn net.Conn, client *ssh.Client) {
	remoteAddr := net.JoinHostPort(strings.TrimSpace(f.tunnel.RemoteHost), strconv.Itoa(f.tunnel.RemotePort))
	remoteConn, err := client.Dial("tcp", remoteAddr)
	if err != nil {
		_ = localConn.Close()
		return
	}

	bridge(localConn, remoteConn)
}

func (f *LocalForward) handleDynamicConn(localConn net.Conn, client *ssh.Client) {
	targetAddr, err := readSOCKS5ConnectTarget(localConn)
	if err != nil {
		_ = localConn.Close()
		return
	}

	remoteConn, err := client.Dial("tcp", targetAddr)
	if err != nil {
		_ = writeSOCKS5Reply(localConn, socksReplyGeneralFailure)
		_ = localConn.Close()
		return
	}

	if err := writeSOCKS5Reply(localConn, socksReplySucceeded); err != nil {
		_ = remoteConn.Close()
		_ = localConn.Close()
		return
	}

	bridge(localConn, remoteConn)
}

func (f *LocalForward) handleRemoteConn(remoteConn net.Conn) {
	localHost := strings.TrimSpace(f.tunnel.LocalHost)
	if localHost == "" {
		localHost = "127.0.0.1"
	}
	localAddr := net.JoinHostPort(localHost, strconv.Itoa(f.tunnel.LocalPort))
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		_ = remoteConn.Close()
		return
	}
	bridge(remoteConn, localConn)
}

func (f *LocalForward) monitorClientLifecycle(client *ssh.Client) {
	defer f.closeEvents()

	for {
		disconnectErr := f.waitClientLoss(client)
		if disconnectErr == nil || f.isStopping() {
			return
		}

		slog.Warn("tunnel connection lost", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "err", disconnectErr)
		f.emitEvent(RuntimeEvent{
			Type: RuntimeEventDisconnected,
			Err:  disconnectErr,
		})
		f.setClient(nil, nil)

		reconnectedClient, reconnectClose, reconnectErr := f.reconnectWithBackoff()
		if reconnectErr != nil {
			f.setRunErr(fmt.Errorf("%v: %w", disconnectErr, reconnectErr))
			slog.Error("tunnel reconnect failed", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "err", reconnectErr)
			f.closeListener()
			return
		}
		if reconnectedClient == nil {
			return
		}
		f.emitEvent(RuntimeEvent{
			Type: RuntimeEventReconnected,
		})
		slog.Info("tunnel reconnected", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name)
		f.setClient(reconnectedClient, reconnectClose)
		client = reconnectedClient
	}
}

func (f *LocalForward) waitClientLoss(client *ssh.Client) error {
	if client == nil {
		return nil
	}
	stop := f.stopSignal()
	if stop == nil {
		return nil
	}

	lost := make(chan error, 1)
	clientDone := make(chan struct{})
	var once sync.Once

	report := func(err error) {
		if err == nil {
			err = errors.New("ssh connection closed")
		}
		once.Do(func() {
			select {
			case lost <- err:
			default:
			}
		})
	}

	go func() {
		err := client.Wait()
		if err != nil {
			report(fmt.Errorf("ssh connection closed: %w", err))
			return
		}
		report(nil)
	}()

	interval := keepAliveInterval(f.lastJumper())
	if interval > 0 {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			if !f.runKeepAliveProbe(client, stop, clientDone, interval, report) {
				return
			}
			for {
				select {
				case <-clientDone:
					return
				case <-stop:
					return
				case <-ticker.C:
					if !f.runKeepAliveProbe(client, stop, clientDone, interval, report) {
						return
					}
				}
			}
		}()
	}

	select {
	case err := <-lost:
		close(clientDone)
		return err
	case <-stop:
		close(clientDone)
		return nil
	}
}

func (f *LocalForward) reconnectWithBackoff() (*ssh.Client, func(), error) {
	stop := f.stopSignal()
	if stop == nil {
		return nil, nil, nil
	}

	deadline := time.Now().Add(reconnectTimeout)
	wait := initReconnectWait
	var lastErr error
	attempt := 0

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		if !waitOrStop(minDuration(wait, remaining), stop) {
			return nil, nil, nil
		}
		attempt++
		slog.Info("tunnel reconnect attempt", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "attempt", attempt, "wait", wait.String())

		client, closeChain, err := dialSSHChain(f.jumpers)
		if err == nil {
			if normalizeForwardMode(f.tunnel.Mode) == "remote" {
				ln, listenErr := f.bindRemoteListener(client)
				if listenErr != nil {
					closeChain()
					lastErr = listenErr
					slog.Warn("tunnel remote listen rebind failed", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "attempt", attempt, "err", listenErr)
					wait = nextReconnectWait(wait)
					continue
				}
				f.replaceListener(ln)
			}
			slog.Info("tunnel reconnect succeeded", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "attempt", attempt)
			return client, closeChain, nil
		}

		lastErr = err
		slog.Warn("tunnel reconnect failed", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "attempt", attempt, "err", err)
		wait = nextReconnectWait(wait)
	}

	if lastErr == nil {
		lastErr = errors.New("reconnect timeout")
	}
	return nil, nil, fmt.Errorf("reconnect timeout after %s: %w", reconnectTimeout, lastErr)
}

func nextReconnectWait(current time.Duration) time.Duration {
	if current <= 0 {
		return initReconnectWait
	}
	next := current * 2
	if next > maxReconnectWait {
		return maxReconnectWait
	}
	return next
}

func waitOrStop(wait time.Duration, stop <-chan struct{}) bool {
	if wait <= 0 {
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-stop:
		return false
	case <-timer.C:
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}

func keepAliveInterval(jumper model.Jumper) time.Duration {
	interval := time.Duration(jumper.KeepAliveIntervalMs) * time.Millisecond
	if interval > 0 {
		return interval
	}
	return 0
}

func (f *LocalForward) lastJumper() model.Jumper {
	if len(f.jumpers) == 0 {
		return model.Jumper{}
	}
	return f.jumpers[len(f.jumpers)-1]
}

func keepAliveRequestTimeout(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 5 * time.Second
	}
	timeout := interval / 2
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	return timeout
}

func (f *LocalForward) runKeepAliveProbe(
	client *ssh.Client,
	stop <-chan struct{},
	clientDone <-chan struct{},
	interval time.Duration,
	report func(err error),
) bool {
	timeout := keepAliveRequestTimeout(interval)
	type keepAliveResult struct {
		latency time.Duration
		err     error
	}
	result := make(chan keepAliveResult, 1)

	go func() {
		latency, err := TestJumperLatency(client)
		result <- keepAliveResult{latency: latency, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-clientDone:
		return false
	case <-stop:
		return false
	case probe := <-result:
		if probe.err == nil {
			f.setLastLatency(probe.latency)
			slog.Debug("tunnel keepalive probe ok", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name)
			return true
		}
		if f.isStopping() {
			return false
		}
		report(fmt.Errorf("keepalive failed: %w", probe.err))
		slog.Warn("tunnel keepalive failed", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "err", probe.err)
		_ = client.Close()
		return false
	case <-timer.C:
		if f.isStopping() {
			return false
		}
		timeoutErr := fmt.Errorf("keepalive timeout after %s", timeout)
		report(timeoutErr)
		slog.Warn("tunnel keepalive timeout", "tunnel_id", f.tunnel.ID, "name", f.tunnel.Name, "timeout", timeout.String())
		_ = client.Close()
		return false
	}
}

func (f *LocalForward) stopSignal() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keepStop
}

func (f *LocalForward) setClient(client *ssh.Client, closeFn func()) {
	f.mu.Lock()
	oldClose := f.clientClose
	defer f.mu.Unlock()
	f.client = client
	f.clientClose = closeFn
	f.lastLatency = 0
	if oldClose != nil {
		go oldClose()
	}
}

func (f *LocalForward) setLastLatency(latency time.Duration) {
	if latency <= 0 {
		return
	}
	f.mu.Lock()
	f.lastLatency = latency
	f.mu.Unlock()
}

func (f *LocalForward) replaceListener(listener net.Listener) {
	f.mu.Lock()
	old := f.listener
	f.listener = listener
	f.mu.Unlock()
	if old != nil && old != listener {
		_ = old.Close()
	}
}

func (f *LocalForward) closeListener() {
	f.mu.Lock()
	ln := f.listener
	f.listener = nil
	f.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

func (f *LocalForward) emitEvent(event RuntimeEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events == nil {
		return
	}
	select {
	case f.events <- event:
	default:
	}
}

func (f *LocalForward) closeEvents() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events == nil {
		return
	}
	close(f.events)
	f.events = nil
}

func (f *LocalForward) isStopping() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopping
}

func (f *LocalForward) setRunErr(err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runErr == nil {
		f.runErr = err
	}
}

func normalizeForwardMode(raw string) string {
	mode := strings.TrimSpace(raw)
	if mode == "" {
		return "local"
	}
	return mode
}

func (f *LocalForward) bindRemoteListener(client *ssh.Client) (net.Listener, error) {
	host := strings.TrimSpace(f.tunnel.RemoteHost)
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(f.tunnel.RemotePort))
	ln, err := client.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("remote listen %s failed: %w", addr, err)
	}
	return ln, nil
}

func bridge(c1, c2 net.Conn) {
	defer c1.Close()
	defer c2.Close()

	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(c1, c2)
		done <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(c2, c1)
		done <- struct{}{}
	}()

	<-done
}

func dialSSH(jumper model.Jumper) (*ssh.Client, error) {
	conf, err := makeSSHClientConfig(jumper)
	if err != nil {
		return nil, err
	}

	host := strings.TrimSpace(jumper.Host)
	if host == "" {
		return nil, fmt.Errorf("jumper host is required")
	}
	port := jumper.Port
	if port <= 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	client, err := ssh.Dial("tcp", addr, conf)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s failed: %w", addr, err)
	}
	return client, nil
}

func dialSSHChain(jumpers []model.Jumper) (*ssh.Client, func(), error) {
	if len(jumpers) == 0 {
		return nil, nil, fmt.Errorf("at least one jumper is required")
	}

	clients := make([]*ssh.Client, 0, len(jumpers))
	first, err := dialSSH(jumpers[0])
	if err != nil {
		return nil, nil, err
	}
	clients = append(clients, first)
	current := first

	closeOnce := sync.Once{}
	closeAll := func() {
		closeOnce.Do(func() {
			for i := len(clients) - 1; i >= 0; i-- {
				_ = clients[i].Close()
			}
		})
	}

	for i := 1; i < len(jumpers); i++ {
		next := jumpers[i]
		conf, err := makeSSHClientConfig(next)
		if err != nil {
			closeAll()
			return nil, nil, err
		}

		host := strings.TrimSpace(next.Host)
		if host == "" {
			closeAll()
			return nil, nil, fmt.Errorf("jumper[%d] host is required", i)
		}
		port := next.Port
		if port <= 0 {
			port = 22
		}
		addr := net.JoinHostPort(host, strconv.Itoa(port))

		conn, err := current.Dial("tcp", addr)
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("ssh dial %s via hop %d failed: %w", addr, i, err)
		}

		cconn, chans, reqs, err := ssh.NewClientConn(conn, addr, conf)
		if err != nil {
			_ = conn.Close()
			closeAll()
			return nil, nil, fmt.Errorf("ssh handshake %s via hop %d failed: %w", addr, i, err)
		}

		client := ssh.NewClient(cconn, chans, reqs)
		clients = append(clients, client)
		current = client
	}

	return current, closeAll, nil
}

func makeSSHClientConfig(jumper model.Jumper) (*ssh.ClientConfig, error) {
	user := strings.TrimSpace(jumper.User)
	if user == "" {
		return nil, fmt.Errorf("jumper user is required")
	}

	auth, err := makeAuthMethod(jumper)
	if err != nil {
		return nil, err
	}

	cb, err := makeHostKeyCallback(jumper.BypassHostVerification)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(jumper.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: cb,
		Timeout:         timeout,
	}

	// 添加HostKeyAlgorithms支持
	if jumper.HostKeyAlgorithms != "" {
		algorithms := parseHostKeyAlgorithms(jumper.HostKeyAlgorithms)
		config.HostKeyAlgorithms = algorithms
	}

	return config, nil
}

func makeAuthMethod(jumper model.Jumper) (ssh.AuthMethod, error) {
	switch strings.TrimSpace(jumper.AuthType) {
	case "password":
		if jumper.Password == "" {
			return nil, fmt.Errorf("password auth requires password")
		}
		return ssh.Password(jumper.Password), nil
	case "ssh_key":
		signer, err := loadPrivateSigner(jumper.KeyPath, jumper.Password)
		if err != nil {
			return nil, err
		}
		return ssh.PublicKeys(ensureSignerSupportsLegacyRSA(signer)), nil
	case "ssh_agent":
		return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			return getSSHAgentSigners(jumper)
		}), nil
	default:
		return nil, fmt.Errorf("unsupported authType: %s", jumper.AuthType)
	}
}

func getSSHAgentSigners(jumper model.Jumper) ([]ssh.Signer, error) {
	a, sock, err := getSSHAgent(jumper.AgentSocketPath)
	if err != nil {
		return nil, err
	}

	signers, err := a.Signers()
	if err != nil {
		// Agent may have restarted while the app keeps running.
		resetSSHAgent()
		var retryErr error
		a, sock, retryErr = getSSHAgent(jumper.AgentSocketPath)
		if retryErr != nil {
			return nil, retryErr
		}
		signers, err = a.Signers()
		if err != nil {
			return nil, fmt.Errorf("load identities from SSH agent failed (%s): %w", sock, err)
		}
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("ssh agent has no identities; selected socket=%s; run ssh-add first", sock)
	}
	for i := range signers {
		signers[i] = ensureSignerSupportsLegacyRSA(signers[i])
	}
	return signers, nil
}

func ensureSignerSupportsLegacyRSA(signer ssh.Signer) ssh.Signer {
	if signer == nil {
		return nil
	}
	pub := signer.PublicKey()
	if pub == nil || pub.Type() != ssh.KeyAlgoRSA {
		return signer
	}
	algorithmSigner, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return signer
	}
	compatibleSigner, err := ssh.NewSignerWithAlgorithms(algorithmSigner, []string{
		ssh.KeyAlgoRSASHA512,
		ssh.KeyAlgoRSASHA256,
		ssh.KeyAlgoRSA,
	})
	if err != nil {
		return signer
	}
	return compatibleSigner
}

func makeHostKeyCallback(bypass bool) (ssh.HostKeyCallback, error) {
	if bypass {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir failed: %w", err)
	}
	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")

	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("known_hosts load failed (%s): %w", knownHostsPath, err)
	}
	return cb, nil
}

func loadPrivateSigner(keyPath, passphrase string) (ssh.Signer, error) {
	resolvedPath, err := resolveKeyPath(keyPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read key file failed (%s): %w", resolvedPath, err)
	}

	if passphrase != "" {
		signer, passErr := ssh.ParsePrivateKeyWithPassphrase(raw, []byte(passphrase))
		if passErr == nil {
			return signer, nil
		}
		signer, keyErr := ssh.ParsePrivateKey(raw)
		if keyErr == nil {
			return signer, nil
		}
		return nil, fmt.Errorf("parse key failed: %v / %v", passErr, keyErr)
	}

	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("parse key failed: %w", err)
	}
	return signer, nil
}

func resolveKeyPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("ssh_key auth requires keyPath")
	}

	if strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir failed: %w", err)
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))
	}

	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed), nil
	}

	if _, err := os.Stat(trimmed); err == nil {
		return filepath.Clean(trimmed), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Clean(trimmed), nil
	}
	fromSSHDir := filepath.Join(home, ".ssh", trimmed)
	if _, err := os.Stat(fromSSHDir); err == nil {
		return filepath.Clean(fromSSHDir), nil
	}

	return filepath.Clean(trimmed), nil
}

func getSSHAgent(preferredSocketPath string) (agent.ExtendedAgent, string, error) {
	sshAgentMu.Lock()
	defer sshAgentMu.Unlock()

	candidates := agentSocketCandidates(preferredSocketPath)
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("no SSH agent socket found; set agent socket path, SSH_AUTH_SOCK, or LORIS_TUNNEL_SSH_AUTH_SOCK")
	}

	if sshAgentInst != nil {
		if signers, err := sshAgentInst.Signers(); err == nil && len(signers) > 0 && socketInCandidates(sshAgentSock, candidates) {
			return sshAgentInst, sshAgentSock, nil
		}
		resetSSHAgentLocked()
	}

	var failures []string
	for _, sock := range candidates {
		conn, err := dialSSHAgentSocket(sock)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", sock, err))
			continue
		}

		inst := agent.NewClient(conn)
		signers, err := inst.Signers()
		if err != nil {
			_ = conn.Close()
			failures = append(failures, fmt.Sprintf("%s: %v", sock, err))
			continue
		}
		if len(signers) == 0 {
			_ = conn.Close()
			failures = append(failures, fmt.Sprintf("%s: no identities", sock))
			continue
		}

		sshAgentInst = inst
		sshAgentSock = sock
		sshAgentConn = conn
		return sshAgentInst, sshAgentSock, nil
	}

	return nil, "", fmt.Errorf("ssh agent has no usable identities; tried: %s", strings.Join(failures, "; "))
}

func agentSocketCandidates(preferredSocketPath string) []string {
	seen := map[string]struct{}{}
	var candidates []string
	add := func(sock string) {
		normalized := normalizeAgentSocketPath(sock)
		if normalized == "" {
			return
		}
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, normalized)
	}

	add(preferredSocketPath)
	add(os.Getenv("LORIS_TUNNEL_SSH_AUTH_SOCK"))
	add(os.Getenv("SSH_AUTH_SOCK"))

	for _, s := range defaultAgentSocketCandidates() {
		add(s)
	}

	if runtime.GOOS != "windows" {
		home, err := os.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			add(filepath.Join(home, ".ssh", "ssh-agent.sock"))
		}
	}

	return candidates
}

func socketInCandidates(sock string, candidates []string) bool {
	normalizedSock := normalizeAgentSocketPath(sock)
	for _, candidate := range candidates {
		if candidate == normalizedSock {
			return true
		}
	}
	return false
}

func normalizeAgentSocketPath(sock string) string {
	s := strings.TrimSpace(sock)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return s
		}
		return filepath.Join(home, strings.TrimPrefix(s, "~/"))
	}
	return s
}

func resetSSHAgent() {
	sshAgentMu.Lock()
	defer sshAgentMu.Unlock()
	resetSSHAgentLocked()
}

func resetSSHAgentLocked() {
	if sshAgentConn != nil {
		_ = sshAgentConn.Close()
	}
	sshAgentInst = nil
	sshAgentSock = ""
	sshAgentConn = nil
}

func splitAlgorithms(s string) []string {
	return strings.Split(s, ",")
}

// parseHostKeyAlgorithms 解析HostKeyAlgorithms字符串，支持OpenSSH风格的语法：
// - "+ssh-rsa"：在默认算法基础上添加ssh-rsa
// - "ssh-rsa"：只使用ssh-rsa（替换默认算法）
// - "-ssh-rsa"：从默认算法中移除ssh-rsa
// - 逗号分隔的列表：直接使用该列表
func parseHostKeyAlgorithms(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	// 检查是否是OpenSSH风格的语法
	if strings.HasPrefix(s, "+") {
		// "+ssh-rsa" 语法：在默认算法基础上添加
		algorithm := strings.TrimPrefix(s, "+")
		return append(getDefaultHostKeyAlgorithms(), algorithm)
	} else if strings.HasPrefix(s, "-") {
		// "-ssh-rsa" 语法：从默认算法中移除
		algorithm := strings.TrimPrefix(s, "-")
		return removeFromDefaultHostKeyAlgorithms(algorithm)
	} else {
		// 普通逗号分隔的列表
		return splitAlgorithms(s)
	}
}

// getDefaultHostKeyAlgorithms 返回Go SSH默认支持的主机密钥算法
func getDefaultHostKeyAlgorithms() []string {
	// Go SSH默认支持的主机密钥算法
	return []string{
		"ssh-rsa",
		"rsa-sha2-256",
		"rsa-sha2-512",
		"ecdsa-sha2-nistp256",
		"ecdsa-sha2-nistp384",
		"ecdsa-sha2-nistp521",
		"ssh-ed25519",
	}
}

// removeFromDefaultHostKeyAlgorithms 从默认算法列表中移除指定的算法
func removeFromDefaultHostKeyAlgorithms(algorithm string) []string {
	defaultAlgos := getDefaultHostKeyAlgorithms()
	var result []string
	for _, algo := range defaultAlgos {
		if algo != algorithm {
			result = append(result, algo)
		}
	}
	return result
}
