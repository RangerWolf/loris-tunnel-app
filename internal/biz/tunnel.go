package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"loris-tunnel/internal/frp"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"loris-tunnel/internal/climaint"
	"loris-tunnel/internal/cloudflared"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

var (
	ErrTunnelNotFound       = errors.New("tunnel not found")
	ErrFreePlanRunningLimit = errors.New("running tunnel limit exceeded")
)

// FreePlanRunningLimit applies to free users across all providers.
// MaxRunningTunnels is retained as an alias for existing callers.
const (
	MaxRunningTunnels    = 3
	FreePlanRunningLimit = MaxRunningTunnels
)

// tunnelRuntime is the small set of operations every provider must support.
// SSH latency and byte counters stay optional and are read with a type assert.
type tunnelRuntime interface {
	Stop() error
	Done() <-chan struct{}
	Err() error
	Events() <-chan forward.RuntimeEvent
}

type latencyRuntime interface {
	LastLatency() (time.Duration, bool)
}

type trafficRuntime interface {
	Traffic() (up, down uint64)
}

type urlRuntime interface {
	PublicURL() string
}

type TunnelBiz struct {
	// ponytail: serialize config edits and start registration; split per record if contention appears.
	devConfigMu        sync.Mutex
	cliMu              sync.Mutex
	cliManagers        map[string]*climaint.Manager
	cliChanging        map[string]bool
	devSessionMu       sync.Mutex
	devSessions        map[string]*devServiceSession
	resourceMu         sync.Mutex
	resourceLocks      map[string]*sync.Mutex
	deletionMu         sync.Mutex
	deletionBusy       map[string]bool
	beforeForgetRecord func()
	newDevServiceHost  func(string, []devtunnel.ServiceTarget) devServiceHost
	devCacheMu         sync.Mutex
	cloudflareCacheMu  sync.Mutex
	cloudflareAuthMu   sync.Mutex
	devAccountKnown    bool
	devAccount         string
	frp                *frp.Client
	frpMu              sync.Mutex
	storage            *conf.Storage
	dev                *devtunnel.Client
	cloudflare         *cloudflared.Client
	mu                 sync.Mutex
	runs               map[int]tunnelRuntime
	runProvider        map[int]string
	inflight           map[int]tunnelRuntime
	starting           map[int]*model.Tunnel
	publicLatency      map[int]*publicLatency
	probeWG            sync.WaitGroup
	knownURL           map[int]string
	startWG            sync.WaitGroup
	devStartCancel     map[*model.Tunnel]context.CancelFunc
}

func NewTunnelBiz(storage *conf.Storage) *TunnelBiz {
	configDir := ""
	if storage != nil {
		configDir = filepath.Dir(storage.Path())
	}
	return &TunnelBiz{
		storage:        storage,
		frp:            &frp.Client{Store: &frp.Store{Path: filepath.Join(configDir, "frp-local.json")}},
		dev:            devtunnel.NewClient(""),
		cloudflare:     cloudflared.NewClient(""),
		runs:           make(map[int]tunnelRuntime),
		runProvider:    make(map[int]string),
		inflight:       make(map[int]tunnelRuntime),
		starting:       make(map[int]*model.Tunnel),
		publicLatency:  make(map[int]*publicLatency),
		knownURL:       make(map[int]string),
		devStartCancel: make(map[*model.Tunnel]context.CancelFunc),
	}
}

func (b *TunnelBiz) List() ([]model.Tunnel, error) {
	cfg, err := b.storage.Load()
	if err != nil {
		return nil, err
	}

	items := append([]model.Tunnel{}, cfg.Tunnels...)
	b.AttachRuntimeState(items)
	return items, nil
}

func (b *TunnelBiz) Create(payload model.TunnelPayload) (model.Tunnel, error) {
	b.devConfigMu.Lock()
	defer b.devConfigMu.Unlock()
	payload = normalizeTunnelPayload(payload)
	if payload.Provider == model.ProviderCloudflare {
		b.cloudflareAuthMu.Lock()
		defer b.cloudflareAuthMu.Unlock()
	}
	if payload.Provider == model.ProviderFRP {
		b.frpMu.Lock()
		defer b.frpMu.Unlock()
	}

	var created model.Tunnel
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		if err := validateTunnelPayload(payload); err != nil {
			return err
		}
		if payload.Provider == model.ProviderSSH {
			if _, err := collectJumpers(cfg.Jumpers, payload.JumperIDs); err != nil {
				return err
			}
		}
		if err := validateGroupID(cfg.Groups, payload.GroupID); err != nil {
			return err
		}
		if err := ensureDevTunnelIDAvailable(cfg.Tunnels, 0, payload); err != nil {
			return err
		}
		if err := ensureCloudflareAvailable(cfg.Tunnels, 0, payload); err != nil {
			return err
		}

		if err := b.validateFRPBinding(cfg.Tunnels, 0, payload); err != nil {
			return err
		}
		created = tunnelFromPayload(nextTunnelID(cfg.Tunnels), payload, nil)
		cfg.Tunnels = append(cfg.Tunnels, created)
		return nil
	})
	if err != nil {
		return model.Tunnel{}, err
	}

	return created, nil
}

func (b *TunnelBiz) Update(id int, payload model.TunnelPayload) (model.Tunnel, error) {
	b.devConfigMu.Lock()
	defer b.devConfigMu.Unlock()
	if id <= 0 {
		return model.Tunnel{}, fmt.Errorf("invalid tunnel id")
	}
	if b.isRunning(id) {
		return model.Tunnel{}, fmt.Errorf("tunnel is running, stop it before editing")
	}

	if current, ok := b.tunnelByID(id); ok && current.DevTunnel != nil && current.DevTunnel.TunnelID != "" {
		unlock := b.lockResource(resourceLockKey(model.ProviderDevTunnel, current.DevTunnel.TunnelID))
		defer unlock()
	}
	payload = normalizeTunnelPayload(payload)
	if payload.Provider == model.ProviderCloudflare {
		b.cloudflareAuthMu.Lock()
		defer b.cloudflareAuthMu.Unlock()
	}
	if payload.Provider == model.ProviderFRP {
		b.frpMu.Lock()
		defer b.frpMu.Unlock()
	}

	var updated model.Tunnel
	var previousTunnelID string
	var previousCloudflareID string
	var previousHostname string
	var devPortChanged bool
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		if err := validateTunnelPayload(payload); err != nil {
			return err
		}
		if payload.Provider == model.ProviderSSH {
			if _, err := collectJumpers(cfg.Jumpers, payload.JumperIDs); err != nil {
				return err
			}
		}
		if err := validateGroupID(cfg.Groups, payload.GroupID); err != nil {
			return err
		}

		idx := -1
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrTunnelNotFound
		}
		if cfg.Tunnels[idx].ProviderName() != payload.Provider {
			return fmt.Errorf("provider cannot be changed")
		}
		if err := ensureDevTunnelIDAvailable(cfg.Tunnels, id, payload); err != nil {
			return err
		}
		if err := ensureCloudflareAvailable(cfg.Tunnels, id, payload); err != nil {
			return err
		}
		if cfg.Tunnels[idx].DevTunnel != nil {
			previousTunnelID = cfg.Tunnels[idx].DevTunnel.TunnelID
		}
		if cfg.Tunnels[idx].Cloudflare != nil {
			previousCloudflareID = cfg.Tunnels[idx].Cloudflare.TunnelID
			previousHostname = cfg.Tunnels[idx].Cloudflare.Hostname
		}

		if err := b.validateFRPBinding(cfg.Tunnels, id, payload); err != nil {
			return err
		}
		devPortChanged = payload.Provider == model.ProviderDevTunnel && cfg.Tunnels[idx].LocalPort != payload.LocalPort
		updated = tunnelFromPayload(id, payload, &cfg.Tunnels[idx])
		if updated.DevTunnel != nil {
			if err := ensureDevTunnelIDAvailable(cfg.Tunnels, id, model.TunnelPayload{Provider: updated.ProviderName(), DevTunnel: updated.DevTunnel}); err != nil {
				return err
			}
		}

		if previousTunnelID != "" && updated.DevTunnel != nil && !devtunnel.SameTunnelID(previousTunnelID, updated.DevTunnel.TunnelID) {
			return fmt.Errorf("tunnel id cannot be changed")
		}
		if previousCloudflareID != "" && updated.Cloudflare != nil && !cloudflared.SameTunnelID(previousCloudflareID, updated.Cloudflare.TunnelID) {
			return fmt.Errorf("tunnel id cannot be changed")
		}
		cfg.Tunnels[idx] = updated
		return nil
	})
	if err != nil {
		return model.Tunnel{}, err
	}
	nextTunnelIDValue := ""
	if updated.DevTunnel != nil {
		nextTunnelIDValue = updated.DevTunnel.TunnelID
	}
	nextCloudflareID := ""
	nextHostname := ""
	if updated.Cloudflare != nil {
		nextCloudflareID = updated.Cloudflare.TunnelID
		nextHostname = updated.Cloudflare.Hostname
	}
	cloudflareIDChanged := (previousCloudflareID != "" || nextCloudflareID != "") && !cloudflared.SameTunnelID(previousCloudflareID, nextCloudflareID)
	if devPortChanged || !devtunnel.SameTunnelID(previousTunnelID, nextTunnelIDValue) || cloudflareIDChanged || previousHostname != nextHostname {
		b.forgetURL(id)
	}

	return updated, nil
}

func (b *TunnelBiz) MoveToGroup(id int, groupID int) (model.Tunnel, error) {
	if id <= 0 {
		return model.Tunnel{}, fmt.Errorf("invalid tunnel id")
	}
	if groupID < 0 {
		groupID = 0
	}

	var updated model.Tunnel
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		if err := validateGroupID(cfg.Groups, groupID); err != nil {
			return err
		}

		idx := -1
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrTunnelNotFound
		}

		cfg.Tunnels[idx].GroupID = groupID
		updated = cfg.Tunnels[idx]
		return nil
	})
	if err != nil {
		return model.Tunnel{}, err
	}

	return updated, nil
}

func (b *TunnelBiz) Delete(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid tunnel id")
	}
	if err := b.stopRuntime(id); err != nil {
		return err
	}

	_, err := b.storage.Update(func(cfg *conf.Config) error {
		idx := -1
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrTunnelNotFound
		}

		cfg.Tunnels = append(cfg.Tunnels[:idx], cfg.Tunnels[idx+1:]...)
		return nil
	})
	if err == nil {
		b.forgetURL(id)
	}
	return err
}

func (b *TunnelBiz) RunningCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.runs) + len(b.starting)
}

// Toggle starts or stops a tunnel. resolveMaxRunning is called only when starting.
// A non-positive limit allows unlimited concurrent tunnels.
func (b *TunnelBiz) Toggle(id int, resolveMaxRunning func() int) (model.Tunnel, error) {
	if id <= 0 {
		return model.Tunnel{}, fmt.Errorf("invalid tunnel id")
	}

	cfg, err := b.storage.Load()
	if err != nil {
		return model.Tunnel{}, err
	}

	tunnel, ok := findTunnelByID(cfg.Tunnels, id)
	if !ok {
		return model.Tunnel{}, ErrTunnelNotFound
	}

	if b.isRunning(id) {
		slog.Info("tunnel toggle stop", "tunnel_id", tunnel.ID, "name", tunnel.Name)
		if err := b.stopRuntime(id); err != nil {
			return model.Tunnel{}, err
		}
		manuallyStopped := true
		return b.updateStatusWithIntent(id, "stopped", "", &manuallyStopped)
	}

	maxRunning := effectiveRunningLimit(resolveMaxRunning())
	if maxRunning > 0 && b.RunningCount() >= maxRunning {
		return model.Tunnel{}, fmt.Errorf("%w: limit %d", ErrFreePlanRunningLimit, maxRunning)
	}
	manuallyStopped := false
	if _, err := b.updateStatusWithIntent(id, tunnel.Status, tunnel.LastError, &manuallyStopped); err != nil {
		return model.Tunnel{}, err
	}

	switch tunnel.ProviderName() {
	case model.ProviderDevTunnel, model.ProviderCloudflare, model.ProviderFRP:
		return b.beginAsyncStart(tunnel, nil, maxRunning)
	}

	jumpers, err := collectJumpers(cfg.Jumpers, tunnel.JumperIDs)
	if err != nil {
		updated, statusErr := b.updateStatus(id, "error", "jumper not found")
		if statusErr != nil {
			return model.Tunnel{}, ErrJumperNotFound
		}
		return updated, nil
	}
	if tunnel.Mode != "local" && tunnel.Mode != "remote" && tunnel.Mode != "dynamic" {
		msg := fmt.Sprintf("mode %s is not supported yet, only local, remote and dynamic forward are implemented", tunnel.Mode)
		updated, statusErr := b.updateStatus(id, "error", msg)
		if statusErr != nil {
			return model.Tunnel{}, fmt.Errorf("%s", msg)
		}
		return updated, nil
	}
	return b.beginAsyncStart(tunnel, jumpers, maxRunning)
}

func (b *TunnelBiz) TestConnection(payload model.TunnelPayload, inlineJumper *model.JumperPayload) (time.Duration, error) {
	payload = normalizeTunnelPayload(payload)
	if payload.Status == "" {
		payload.Status = "stopped"
	}
	if payload.Provider == model.ProviderFRP {
		started := time.Now()
		err := probeLocalOrigin(payload.LocalHost, payload.LocalPort)
		return time.Since(started), err
	}
	if payload.Provider == model.ProviderDevTunnel || payload.Provider == model.ProviderCloudflare {
		return testDevTunnelLocal(payload)
	}
	allowEmptyJumpers := inlineJumper != nil
	if err := validateTunnelPayloadWithOption(payload, !allowEmptyJumpers); err != nil {
		return 0, err
	}

	chain := make([]model.Jumper, 0, len(payload.JumperIDs)+1)
	var inline model.Jumper
	hasInline := false
	if inlineJumper != nil {
		jumperPayload := normalizeJumperPayload(*inlineJumper)
		if err := validateJumperPayload(jumperPayload); err != nil {
			return 0, fmt.Errorf("jumper: %w", err)
		}
		inline = model.Jumper{
			Name:                   jumperPayload.Name,
			Host:                   jumperPayload.Host,
			Port:                   jumperPayload.Port,
			User:                   jumperPayload.User,
			AuthType:               jumperPayload.AuthType,
			KeyPath:                jumperPayload.KeyPath,
			AgentSocketPath:        jumperPayload.AgentSocketPath,
			Password:               jumperPayload.Password,
			BypassHostVerification: jumperPayload.BypassHostVerification,
			KeepAliveIntervalMs:    jumperPayload.KeepAliveIntervalMs,
			TimeoutMs:              jumperPayload.TimeoutMs,
			HostKeyAlgorithms:      jumperPayload.HostKeyAlgorithms,
			Notes:                  jumperPayload.Notes,
		}
		hasInline = true
	}
	if len(payload.JumperIDs) > 0 {
		cfg, err := b.storage.Load()
		if err != nil {
			return 0, err
		}
		jumpers, err := collectJumpers(cfg.Jumpers, payload.JumperIDs)
		if err != nil {
			return 0, err
		}
		chain = append(chain, jumpers...)
	}
	if hasInline {
		chain = append(chain, inline)
	}

	t := model.Tunnel{
		Name:       payload.Name,
		Mode:       payload.Mode,
		LocalHost:  payload.LocalHost,
		LocalPort:  payload.LocalPort,
		RemoteHost: payload.RemoteHost,
		RemotePort: payload.RemotePort,
	}
	return forward.TestTunnelConnection(t, chain)
}

// AttachRuntimeState overlays live status on a detached configuration snapshot.
func (b *TunnelBiz) AttachRuntimeState(items []model.Tunnel) {
	if len(items) == 0 {
		return
	}

	b.mu.Lock()
	starting := make(map[int]bool, len(b.starting))
	for id := range b.starting {
		starting[id] = true
	}
	runs := make(map[int]tunnelRuntime, len(b.runs))
	for id, run := range b.runs {
		runs[id] = run
	}
	known := make(map[int]string, len(b.knownURL))
	for id, url := range b.knownURL {
		known[id] = url
	}
	b.mu.Unlock()

	for i := range items {
		if items[i].ProviderName() == model.ProviderFRP && items[i].FRP != nil {
			if server, err := b.frp.Store.Server(items[i].FRP.ServerID); err == nil {
				items[i].PublicURL = frp.Address(server, items[i].Mode, items[i].FRP.RemotePort, items[i].FRP.Domain)
			}
			if origin, ok := runs[items[i].ID].(interface{ OriginState() string }); ok {
				items[i].OriginState = origin.OriginState()
			}
		}
		if items[i].ProviderName() == model.ProviderDevTunnel && items[i].DevTunnel != nil {
			items[i].PublicURL = items[i].DevTunnel.LastPublicURL
			if known[items[i].ID] != "" {
				items[i].PublicURL = known[items[i].ID]
			}
		}
		if starting[items[i].ID] {
			items[i].Status = "busy"
			items[i].LatencyMs = 0
			continue
		}
		if items[i].Status == "busy" {
			if runs[items[i].ID] != nil {
				items[i].Status = "running"
			} else {
				items[i].Status = "stopped"
			}
		}
		if source, ok := runs[items[i].ID].(interface{ ReconnectInfo() *model.ReconnectInfo }); ok {
			items[i].Reconnect = source.ReconnectInfo()
		}
		if items[i].Status != "running" && items[i].Status != "reconnecting" {
			items[i].LatencyMs = 0
			if items[i].ProviderName() == model.ProviderCloudflare && items[i].Cloudflare != nil {
				items[i].PublicURL = cloudflared.PublicURL(items[i].Cloudflare.Hostname)
			}
			continue
		}
		run, ok := runs[items[i].ID]
		if !ok || run == nil {
			// Persisted status can outlive the process that owned the listener.
			items[i].Status = "stopped"
			items[i].LastError = ""
			items[i].LatencyMs = 0
			continue
		}
		if source, ok := run.(urlRuntime); ok {
			items[i].PublicURL = source.PublicURL()
			b.rememberURL(items[i].ID, items[i].PublicURL)
		}
		if items[i].ProviderName() != model.ProviderSSH {
			b.attachPublicLatency(&items[i], run)
			continue
		}
		latencySource, ok := run.(latencyRuntime)
		if !ok {
			items[i].LatencyMs = 0
			continue
		}
		latency, hasLatency := latencySource.LastLatency()
		if !hasLatency || latency <= 0 {
			items[i].LatencyMs = 0
			continue
		}
		items[i].LatencyMs = latency.Milliseconds()
	}
}

func (b *TunnelBiz) TrafficSnapshot() (up, down uint64) {
	b.mu.Lock()
	runs := make([]trafficRuntime, 0, len(b.runs))
	for _, run := range b.runs {
		source, ok := run.(trafficRuntime)
		if ok && source != nil {
			runs = append(runs, source)
		}
	}
	b.mu.Unlock()

	for _, run := range runs {
		runUp, runDown := run.Traffic()
		up += runUp
		down += runDown
	}
	return up, down
}

// StartAutoStart reserves eligible tunnels and starts connections in the background.
// Callers serialize preparation with config mutations using App.configGate.
// Non-positive maxRunning means unlimited. In-flight starts count toward positive limits.
func (b *TunnelBiz) StartAutoStart(maxRunning int) error { return b.startSelected(maxRunning, nil) }
func (b *TunnelBiz) StartPreviouslyRunning(ids []int, maxRunning int) error {
	selected := map[int]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	return b.startSelected(maxRunning, selected)
}
func (b *TunnelBiz) startSelected(maxRunning int, selected map[int]bool) error {
	b.devConfigMu.Lock()
	defer b.devConfigMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	limit := effectiveRunningLimit(maxRunning)
	tasks := []*model.Tunnel{}
	cfg, err := b.storage.UpdateRuntime(func(cfg *conf.Config) error {
		for i := range cfg.Tunnels {
			t := &cfg.Tunnels[i]
			_, running := b.runs[t.ID]
			_, starting := b.starting[t.ID]
			if !running && !starting && (t.Status == "running" || t.Status == "busy" || t.Status == "reconnecting") {
				t.Status = "stopped"
				t.LastError = ""
			}
			eligible := t.AutoStart && !t.ManuallyStopped
			if selected != nil {
				eligible = selected[t.ID]
			}
			if running || starting || !eligible || b.cliChanging[t.ProviderName()] {
				continue
			}
			if err := ensureDevTunnelIDAvailable(cfg.Tunnels, t.ID, model.TunnelPayload{Provider: t.ProviderName(), DevTunnel: t.DevTunnel}); err != nil {
				t.Status = "error"
				t.LastError = err.Error()
				continue
			}
			if limit > 0 && len(b.runs)+len(b.starting)+len(tasks) >= limit {
				continue
			}
			snapshot := *t
			tasks = append(tasks, &snapshot)
			t.Status = "busy"
			t.LastError = ""
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		b.starting[task.ID] = task
		b.startWG.Add(1)
		go b.startAutoRuntime(task, cfg.Jumpers)
	}
	return nil
}

func (b *TunnelBiz) startAutoRuntime(task *model.Tunnel, jumpers []model.Jumper) {
	defer b.startWG.Done()
	b.mu.Lock()
	active := b.starting[task.ID] == task
	b.mu.Unlock()
	if !active {
		return
	}
	var run tunnelRuntime
	var err error
	switch task.ProviderName() {
	case model.ProviderFRP:
		run, err = b.newStartedFRPRuntime(task)
	case model.ProviderDevTunnel:
		run, err = b.newStartedDevRuntime(task)
	case model.ProviderCloudflare:
		run, err = b.newStartedCloudflareRuntime(task)
	case model.ProviderSSH:
		var chain []model.Jumper
		chain, err = collectJumpers(jumpers, task.JumperIDs)
		if err == nil {
			run, err = b.newStartedRuntime(*task, chain)
		}
	default:
		err = fmt.Errorf("unsupported provider: %s", task.ProviderName())
	}
	b.mu.Lock()
	// A stop, deletion, or config replacement invalidates this reservation.
	// A late connection must never restore the canceled tunnel's status.
	if b.starting[task.ID] != task {
		b.mu.Unlock()
		if run != nil {
			_ = run.Stop()
		}
		return
	}
	delete(b.starting, task.ID)
	delete(b.inflight, task.ID)
	if err != nil {
		_, _ = b.updateStatus(task.ID, providerFailureStatus(err), errReason(err))
		b.mu.Unlock()
		return
	}
	b.runs[task.ID] = run
	b.runProvider[task.ID] = task.ProviderName()
	if source, ok := run.(urlRuntime); ok {
		b.knownURL[task.ID] = source.PublicURL()
	}
	_, statusErr := b.updateStatus(task.ID, "running", "")
	if statusErr != nil {
		delete(b.runs, task.ID)
		delete(b.runProvider, task.ID)
	}
	b.mu.Unlock()
	if statusErr == nil && task.ProviderName() == model.ProviderDevTunnel {
		if source, ok := run.(urlRuntime); ok {
			if _, cacheErr := b.storage.Update(func(cfg *conf.Config) error {
				for i := range cfg.Tunnels {
					t := &cfg.Tunnels[i]
					if t.ID == task.ID && t.DevTunnel != nil && task.DevTunnel != nil && devtunnel.SameTunnelID(t.DevTunnel.TunnelID, task.DevTunnel.TunnelID) {
						t.DevTunnel.LastPublicURL = strings.TrimSpace(source.PublicURL())
					}
				}
				return nil
			}); cacheErr != nil {
				slog.Warn("cache dev tunnel public URL failed", "tunnel_id", task.ID, "err", cacheErr)
			}
		}
	}
	if statusErr != nil {
		_ = run.Stop()
		slog.Error("persist auto-start status failed", "tunnel_id", task.ID, "err", statusErr)
		return
	}
	slog.Info("tunnel runtime started", "tunnel_id", task.ID, "name", task.Name)
	b.startPublicLatency(*task, run)
	go b.watchRuntime(task.ID, run)
}

func (b *TunnelBiz) Shutdown() {
	b.mu.Lock()
	ids := make([]int, 0, len(b.runs)+len(b.starting))
	for id := range b.starting {
		ids = append(ids, id)
	}
	for id := range b.runs {
		ids = append(ids, id)
	}
	b.mu.Unlock()

	for _, id := range ids {
		_ = b.stopRuntime(id)
		_, _ = b.updateStatus(id, "stopped", "")
	}
	b.startWG.Wait()
	b.probeWG.Wait()
}

func (b *TunnelBiz) beginAsyncStart(tunnel model.Tunnel, jumpers []model.Jumper, maxRunning int) (model.Tunnel, error) {
	b.devConfigMu.Lock()
	defer b.devConfigMu.Unlock()
	if tunnel.ProviderName() == model.ProviderDevTunnel {
		cfg, err := b.storage.Load()
		if err != nil {
			return model.Tunnel{}, err
		}
		current, ok := findTunnelByID(cfg.Tunnels, tunnel.ID)
		if !ok {
			return model.Tunnel{}, ErrTunnelNotFound
		}
		tunnel = current
		if err := ensureDevTunnelIDAvailable(cfg.Tunnels, tunnel.ID, model.TunnelPayload{Provider: tunnel.ProviderName(), DevTunnel: tunnel.DevTunnel}); err != nil {
			return b.updateStatus(tunnel.ID, "error", err.Error())
		}
	}

	b.mu.Lock()
	if b.cliChanging[tunnel.ProviderName()] {
		b.mu.Unlock()
		return model.Tunnel{}, fmt.Errorf("CLI version maintenance is in progress")
	}
	if _, ok := b.runs[tunnel.ID]; ok || b.starting[tunnel.ID] != nil {
		b.mu.Unlock()
		return tunnel, nil
	}
	if maxRunning > 0 && len(b.runs)+len(b.starting) >= maxRunning {
		b.mu.Unlock()
		return model.Tunnel{}, fmt.Errorf("%w: limit %d", ErrFreePlanRunningLimit, maxRunning)
	}
	task := tunnel
	b.starting[tunnel.ID] = &task
	b.startWG.Add(1)
	b.mu.Unlock()

	updated, err := b.updateStatus(tunnel.ID, "busy", "")
	if err != nil {
		b.mu.Lock()
		if b.starting[tunnel.ID] == &task {
			delete(b.starting, tunnel.ID)
		}
		b.mu.Unlock()
		b.startWG.Done()
		return model.Tunnel{}, err
	}
	go b.startAutoRuntime(&task, jumpers)
	return updated, nil
}

func (b *TunnelBiz) newStartedRuntime(t model.Tunnel, jumpers []model.Jumper) (tunnelRuntime, error) {
	for _, j := range jumpers {
		if j.NeedsCredentials {
			return nil, fmt.Errorf("jumper %s requires local authentication setup", j.Name)
		}
	}
	run := forward.NewLocalForward(t, jumpers)
	if err := run.Start(); err != nil {
		slog.Error("tunnel runtime start failed", "tunnel_id", t.ID, "name", t.Name, "err", err)
		return nil, err
	}
	return run, nil
}

func (b *TunnelBiz) watchRuntime(id int, run tunnelRuntime) {
	done := run.Done()
	if done == nil {
		return
	}

	events := run.Events()
	disconnected := false
	for {
		select {
		case <-done:
			b.mu.Lock()
			active, ok := b.runs[id]
			if !ok || active != run {
				b.mu.Unlock()
				return
			}
			provider := b.runProvider[id]
			b.cancelPublicLatency(id)
			delete(b.runs, id)
			delete(b.runProvider, id)

			if run.Err() != nil {
				slog.Warn("tunnel runtime exited with error", "tunnel_id", id, "err", run.Err())
				status := "error"
				if provider == model.ProviderDevTunnel {
					status = providerFailureStatus(run.Err())
				}
				_, _ = b.updateStatus(id, status, errReason(run.Err()))
			} else {
				slog.Info("tunnel runtime exited", "tunnel_id", id)
			}
			b.mu.Unlock()
			return
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			b.mu.Lock()
			active, stillRunning := b.runs[id]
			if !stillRunning || active != run {
				b.mu.Unlock()
				continue
			}
			switch evt.Type {
			case forward.RuntimeEventDisconnected:
				if evt.Reconnect == nil || evt.Reconnect.Attempt == 1 {
					slog.Warn("tunnel runtime disconnected", "tunnel_id", id, "err", evt.Err)
				} else if !disconnected {
					slog.Warn("tunnel runtime disconnected", "tunnel_id", id, "attempt", evt.Reconnect.Attempt)
				}
				status := "error"
				if b.runProvider[id] == model.ProviderFRP || b.runProvider[id] == model.ProviderDevTunnel {
					status = "reconnecting"
				}
				// Retry progress is runtime-only; persist only the transition.
				if evt.Reconnect == nil || !disconnected {
					_, _ = b.updateStatus(id, status, errReason(evt.Err))
				}
				disconnected = true
			case forward.RuntimeEventReconnected:
				disconnected = false
				slog.Info("tunnel runtime reconnected", "tunnel_id", id)
				_, _ = b.updateStatus(id, "running", "")
			}
			b.mu.Unlock()
		}
	}
}

func (b *TunnelBiz) stopRuntime(id int) error {
	b.mu.Lock()
	cancel := b.devStartCancel[b.starting[id]]
	delete(b.starting, id)
	inflight := b.inflight[id]
	delete(b.inflight, id)
	run, ok := b.runs[id]
	if ok {
		b.cancelPublicLatency(id)
		delete(b.runs, id)
		delete(b.runProvider, id)
	}
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	if inflight != nil && inflight != run {
		_ = inflight.Stop()
	}
	if !ok || run == nil {
		return nil
	}
	return run.Stop()
}

func (b *TunnelBiz) isRunning(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.runs[id]
	_, starting := b.starting[id]
	return ok || starting
}

func (b *TunnelBiz) updateStatus(id int, status, lastError string) (model.Tunnel, error) {
	return b.updateStatusWithIntent(id, status, lastError, nil)
}

// A nil intent preserves the user's choice during internal lifecycle changes.
func (b *TunnelBiz) updateStatusWithIntent(id int, status, lastError string, manuallyStopped *bool) (model.Tunnel, error) {
	var updated model.Tunnel
	update := b.storage.UpdateRuntime
	if manuallyStopped != nil {
		update = b.storage.Update
	}
	_, err := update(func(cfg *conf.Config) error {
		idx := -1
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrTunnelNotFound
		}
		cfg.Tunnels[idx].Status = status
		cfg.Tunnels[idx].LastError = strings.TrimSpace(lastError)
		if manuallyStopped != nil {
			cfg.Tunnels[idx].ManuallyStopped = *manuallyStopped
		}
		updated = cfg.Tunnels[idx]
		return nil
	})
	if err != nil {
		return model.Tunnel{}, err
	}
	if updated.LastError != "" {
		slog.Info("tunnel status updated", "tunnel_id", updated.ID, "name", updated.Name, "status", updated.Status, "error", updated.LastError)
	} else {
		slog.Info("tunnel status updated", "tunnel_id", updated.ID, "name", updated.Name, "status", updated.Status)
	}
	return updated, nil
}

func errReason(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func normalizeTunnelPayload(payload model.TunnelPayload) model.TunnelPayload {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Provider = strings.TrimSpace(payload.Provider)
	payload.Mode = strings.TrimSpace(payload.Mode)
	payload.LocalHost = strings.TrimSpace(payload.LocalHost)
	payload.RemoteHost = strings.TrimSpace(payload.RemoteHost)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.Status = strings.TrimSpace(payload.Status)
	payload.JumperIDs = normalizeJumperIDs(payload.JumperIDs)
	if payload.GroupID < 0 {
		payload.GroupID = 0
	}
	if payload.Provider == "" {
		payload.Provider = model.ProviderSSH
	}
	if payload.Status == "" {
		payload.Status = "stopped"
	}
	if payload.LocalHost == "" {
		payload.LocalHost = "127.0.0.1"
	}
	if payload.Provider == model.ProviderFRP {
		payload.JumperIDs = nil
		payload.DevTunnel = nil
		payload.Cloudflare = nil
		payload.RemoteHost = ""
		payload.RemotePort = 0
		if payload.FRP != nil {
			copied := *payload.FRP
			payload.FRP = &copied
			payload.FRP.ServerID = strings.TrimSpace(payload.FRP.ServerID)
			payload.FRP.ProxyName = strings.TrimSpace(payload.FRP.ProxyName)
			payload.FRP.Domain = frp.DomainName(payload.FRP.Domain)
		}
		return payload
	}
	payload.FRP = nil
	if payload.Provider == model.ProviderDevTunnel {
		payload.JumperIDs = nil
		payload.RemoteHost = ""
		payload.RemotePort = 0
		if payload.Mode == "" {
			payload.Mode = "http"
		}
		if payload.DevTunnel == nil {
			payload.DevTunnel = &model.DevTunnelConfig{}
		}
		payload.DevTunnel.LocalHost = payload.LocalHost
		payload.DevTunnel.LocalPort = payload.LocalPort
		payload.DevTunnel.Protocol = payload.Mode
		payload.DevTunnel.Access = strings.TrimSpace(payload.DevTunnel.Access)
		if payload.DevTunnel.Access == "" {
			payload.DevTunnel.Access = "private"
		}
		expiration, err := devtunnel.NormalizeExpiration(payload.DevTunnel.Expiration)
		if err == nil {
			payload.DevTunnel.Expiration = expiration
		}
		region, regionErr := devtunnel.NormalizeRegion(payload.DevTunnel.Region)
		if regionErr == nil {
			payload.DevTunnel.Region = region
		}
		payload.DevTunnel.TunnelID = strings.TrimSpace(payload.DevTunnel.TunnelID)
		payload.DevTunnel.Account = strings.TrimSpace(payload.DevTunnel.Account)
		payload.DevTunnel.TunnelName = strings.TrimSpace(payload.DevTunnel.TunnelName)
		payload.DevTunnelIDMode = strings.ToLower(strings.TrimSpace(payload.DevTunnelIDMode))
		if payload.DevTunnelIDMode == "auto" {
			payload.DevTunnel.TunnelID = ""
			payload.DevTunnel.TunnelName = ""
		}
		if payload.DevTunnelIDMode == "bind" {
			payload.DevTunnel.Region = ""
			payload.DevTunnel.TunnelName = ""
		}
		if payload.DevTunnelIDMode == "custom" {
			payload.DevTunnel.TunnelName = strings.ToLower(payload.DevTunnel.TunnelName)
		}
		payload.Cloudflare = nil
		return payload
	}
	if payload.Provider == model.ProviderCloudflare {
		payload.JumperIDs = nil
		payload.RemoteHost = ""
		payload.RemotePort = 0
		payload.DevTunnel = nil
		if payload.Mode == "" {
			payload.Mode = "http"
		}
		if payload.Cloudflare == nil {
			payload.Cloudflare = &model.CloudflareTunnelConfig{}
		}
		payload.Cloudflare.LocalHost = payload.LocalHost
		payload.Cloudflare.LocalPort = payload.LocalPort
		payload.Cloudflare.Protocol = payload.Mode
		payload.Cloudflare.TunnelID = strings.TrimSpace(payload.Cloudflare.TunnelID)
		payload.Cloudflare.TunnelName = strings.TrimSpace(payload.Cloudflare.TunnelName)
		if host, err := cloudflared.NormalizeHostname(payload.Cloudflare.Hostname); err == nil {
			payload.Cloudflare.Hostname = host
		} else {
			payload.Cloudflare.Hostname = strings.TrimSpace(payload.Cloudflare.Hostname)
		}
		payload.CloudflareIDMode = strings.ToLower(strings.TrimSpace(payload.CloudflareIDMode))
		if payload.CloudflareIDMode == "auto" {
			payload.Cloudflare.TunnelID = ""
		}
		return payload
	}

	payload.Provider = model.ProviderSSH
	payload.DevTunnel = nil
	payload.Cloudflare = nil
	if payload.Mode == "" {
		payload.Mode = "local"
	}
	return payload
}

func validateTunnelPayload(payload model.TunnelPayload) error {
	return validateTunnelPayloadWithOption(payload, true)
}

func validateTunnelPayloadWithOption(payload model.TunnelPayload, requireJumpers bool) error {
	if payload.Name == "" {
		return fmt.Errorf("name is required")
	}
	if payload.LocalHost == "" {
		return fmt.Errorf("localHost is required")
	}
	if payload.LocalPort < 1 || payload.LocalPort > 65535 {
		return fmt.Errorf("localPort must be between 1 and 65535")
	}
	switch payload.Status {
	case "running", "stopped", "error", "needs_setup", "auth_required", "reconnecting":
	default:
		return fmt.Errorf("unsupported status: %s", payload.Status)
	}
	if payload.Provider == model.ProviderFRP {
		return validateFRPPayload(payload)
	}
	if payload.Provider == model.ProviderDevTunnel {
		return validateDevTunnelPayload(payload)
	}
	if payload.Provider == model.ProviderCloudflare {
		return validateCloudflareTunnelPayload(payload)
	}
	if payload.Provider != model.ProviderSSH {
		return fmt.Errorf("unsupported provider: %s", payload.Provider)
	}
	if requireJumpers && len(payload.JumperIDs) == 0 {
		return fmt.Errorf("jumperIds is required")
	}
	switch payload.Mode {
	case "local", "remote", "dynamic":
	default:
		return fmt.Errorf("unsupported mode: %s", payload.Mode)
	}
	if payload.Mode != "dynamic" {
		if payload.RemoteHost == "" {
			return fmt.Errorf("remoteHost is required for non-dynamic mode")
		}
		if payload.RemotePort < 1 || payload.RemotePort > 65535 {
			return fmt.Errorf("remotePort must be between 1 and 65535")
		}
	}
	return nil
}

func collectJumpers(items []model.Jumper, ids []int) ([]model.Jumper, error) {
	if len(ids) == 0 {
		return nil, ErrJumperNotFound
	}
	collected := make([]model.Jumper, 0, len(ids))
	for _, id := range ids {
		item, ok := findJumperByID(items, id)
		if !ok {
			return nil, ErrJumperNotFound
		}
		collected = append(collected, item)
	}
	return collected, nil
}

func normalizeJumperIDs(ids []int) []int {
	out := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	appendID := func(id int) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range ids {
		appendID(id)
	}
	return out
}

func findJumperByID(items []model.Jumper, id int) (model.Jumper, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return model.Jumper{}, false
}

func findTunnelByID(items []model.Tunnel, id int) (model.Tunnel, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return model.Tunnel{}, false
}

func nextTunnelID(items []model.Tunnel) int {
	next := 1
	for _, item := range items {
		if item.ID >= next {
			next = item.ID + 1
		}
	}
	return next
}

// RunningIDs captures the actual runtime set for rollback, not persisted status.
func (b *TunnelBiz) RunningIDs() []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := []int{}
	for id := range b.starting {
		ids = append(ids, id)
	}
	for id := range b.runs {
		ids = append(ids, id)
	}
	return ids
}

func effectiveRunningLimit(maxRunning int) int {
	if maxRunning < 0 {
		return 0
	}
	return maxRunning
}

func providerFailureStatus(err error) string {
	if errors.Is(err, cloudflared.ErrNotInstalled) || errors.Is(err, cloudflared.ErrCredentialMissing) {
		return "needs_setup"
	}
	if errors.Is(err, cloudflared.ErrAuthRequired) {
		return "auth_required"
	}
	text := strings.ToLower(errReason(err))
	switch {
	case strings.Contains(text, "not installed"), strings.Contains(text, "credentials are missing"):
		return "needs_setup"
	case strings.Contains(text, "login expired"), strings.Contains(text, "login token expired"), strings.Contains(text, "login required"), strings.Contains(text, "not logged in"), strings.Contains(text, "token expired"), strings.Contains(text, "unauthorized"), strings.Contains(text, "not signed"), strings.Contains(text, "authentication is required"):
		return "auth_required"
	default:
		return "error"
	}
}

func (b *TunnelBiz) DevTunnelStatus(ctx context.Context) (devtunnel.Status, error) {
	status, err := b.dev.Inspect(ctx)
	if err == nil {
		b.rememberDevAccount(status)
	}
	return status, err
}

func (b *TunnelBiz) ListDevTunnelResources(ctx context.Context) devtunnel.ResourceList {
	return b.dev.ListResources(ctx)
}

func (b *TunnelBiz) LoadDevTunnelRegions(ctx context.Context, measure, force bool) devtunnel.RegionProbe {
	return b.dev.LoadRegions(ctx, measure, force)
}

func (b *TunnelBiz) GetDevTunnelResource(ctx context.Context, id string) devtunnel.ResourceDetail {
	return b.dev.GetResource(ctx, id)
}

func (b *TunnelBiz) CreateDevTunnelResource(ctx context.Context, input devtunnel.CreateResourceInput) devtunnel.ResourceDetail {
	result := b.dev.CreateResource(ctx, input)
	if result.State == devtunnel.ResourceReady {
		b.updateDevResourceCache(result.Resource, false)
	}
	return result
}

func (b *TunnelBiz) StartDevTunnelLogin(kind string, progress func(phase, url string), done func()) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cliChanging[model.ProviderDevTunnel] {
		return fmt.Errorf("CLI version maintenance is in progress")
	}
	return b.dev.StartLogin(kind, progress, done)
}

func (b *TunnelBiz) CancelDevTunnelLogin() {
	b.dev.CancelLogin()
}

func (b *TunnelBiz) LogoutDevTunnel(ctx context.Context) error {
	b.mu.Lock()
	for _, provider := range b.runProvider {
		if provider == model.ProviderDevTunnel {
			b.mu.Unlock()
			return fmt.Errorf("stop Dev Tunnel connections before signing out")
		}
	}
	for _, task := range b.starting {
		if task != nil && task.ProviderName() == model.ProviderDevTunnel {
			b.mu.Unlock()
			return fmt.Errorf("stop Dev Tunnel connections before signing out")
		}
	}
	b.mu.Unlock()
	err := b.dev.Logout(ctx)
	if err == nil {
		b.rememberDevAccount(devtunnel.Status{AccountState: "signed_out"})
	}
	return err
}

func (b *TunnelBiz) InstallDevTunnelCLI(ctx context.Context) error {
	return b.installManagedCLI(ctx, model.ProviderDevTunnel)
}
func (b *TunnelBiz) CancelDevTunnelInstall() {
	b.CancelCLIUpdate(model.ProviderDevTunnel)
	b.dev.CancelInstall()
}

func (b *TunnelBiz) newStartedDevRuntime(task *model.Tunnel) (tunnelRuntime, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b.mu.Lock()
	if b.starting[task.ID] != task {
		b.mu.Unlock()
		return nil, context.Canceled
	}
	b.devStartCancel[task] = cancel
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.devStartCancel, task); b.mu.Unlock() }()
	if current, ok := b.tunnelByID(task.ID); !ok {
		return nil, ErrTunnelNotFound
	} else if current.DevTunnel != nil {
		if err := b.checkDevTunnelOwner(task.ID, current.DevTunnel.TunnelID); err != nil {
			return nil, err
		}
	}
	if err := b.dev.Preflight(ctx); err != nil {
		return nil, err
	}
	cfg := task.DevTunnel
	if cfg == nil {
		cfg = &model.DevTunnelConfig{Access: "private", Expiration: "30d", Protocol: task.Mode}
		task.DevTunnel = cfg
	}
	port := cfg.LocalPort
	if port == 0 {
		port = task.LocalPort
	}
	protocol := cfg.Protocol
	if protocol == "" {
		protocol = task.Mode
	}
	id := strings.TrimSpace(cfg.TunnelID)
	if id != "" && !devtunnel.ValidTunnelID(id) {
		return nil, fmt.Errorf("invalid dev tunnel id")
	}
	if id == "" {
		expiration, err := devtunnel.NormalizeExpiration(cfg.Expiration)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(cfg.TunnelName)
		if name != "" {
			name, err = devtunnel.NormalizeTunnelName(name)
			if err != nil {
				return nil, err
			}
			cfg.TunnelName = name
		}
		id, err = b.dev.CreatePersistent(ctx, cfg.Access == "public", expiration, cfg.Region, name)
		if err != nil {
			return nil, err
		}
		if b.startCanceled(task.ID) {
			_ = b.dev.DeleteRemote(context.Background(), id)
			return nil, fmt.Errorf("dev tunnel start canceled")
		}
		account := ""
		if status, statusErr := b.dev.Inspect(ctx); statusErr == nil {
			account = status.AccountLabel
			b.rememberDevAccount(status)
		}
		if err := b.persistDevTunnelID(task.ID, id, account); err != nil {
			_ = b.dev.DeleteRemote(context.Background(), id)
			return nil, err
		}
		cfg.TunnelID = id

	}
	unlock := b.lockResource(resourceLockKey(model.ProviderDevTunnel, id))
	defer unlock()
	if err := b.prepareExistingDevTunnel(ctx, task.ID, cfg, id, port, protocol); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(cfg.TunnelID)
	targetHost := cfg.LocalHost
	if targetHost == "" {
		targetHost = task.LocalHost
	}
	runtime := &devServiceRuntime{biz: b, key: strings.ToLower(id), id: task.ID, target: devtunnel.ServiceTarget{Host: targetHost, Port: cfg.TunnelPort, LocalPort: port}, done: make(chan struct{}), events: make(chan forward.RuntimeEvent, 16)}
	b.mu.Lock()
	if b.starting[task.ID] != task {
		b.mu.Unlock()
		return nil, fmt.Errorf("dev tunnel start canceled")
	}
	b.inflight[task.ID] = runtime
	b.mu.Unlock()
	if err := b.startDevServiceLocked(ctx, runtime); err != nil {
		b.mu.Lock()
		delete(b.inflight, task.ID)
		b.mu.Unlock()
		return nil, err
	}
	return runtime, nil
}

// Caller holds the resource lock through reconciliation and host startup.
func (b *TunnelBiz) prepareExistingDevTunnel(ctx context.Context, selfID int, cfg *model.DevTunnelConfig, id string, port int, protocol string) error {
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if b.startCanceled(selfID) {
			return context.Canceled
		}
		current, ok := b.tunnelByID(selfID)
		if !ok {
			return ErrTunnelNotFound
		}
		if current.DevTunnel == nil || !devtunnel.SameTunnelID(current.DevTunnel.TunnelID, id) || current.LocalPort != port || current.LocalHost != cfg.LocalHost || current.Mode != protocol {
			return fmt.Errorf("service configuration changed during startup")
		}
		return b.checkDevTunnelOwner(selfID, id)
	}
	if err := check(); err != nil {
		return err
	}
	detail := b.dev.GetResource(ctx, id)
	if detail.State != devtunnel.ResourceReady {
		return devTunnelResourceError(detail)
	}
	if !detail.Resource.PortsKnown {
		return fmt.Errorf("remote port list is unknown")
	}
	qualified := strings.TrimSpace(detail.Resource.ID)
	if qualified != "" && devtunnel.ValidTunnelID(qualified) {
		if !devtunnel.SameTunnelID(id, qualified) {
			return fmt.Errorf("remote Tunnel ID does not match requested ID")
		}
		id = qualified
	}
	if err := check(); err != nil {
		return err
	}
	published, err := b.allocateDevTunnelPort(cfg.LocalHost, port)
	if err != nil {
		return err
	}
	// Validate the retained port before deleting anything else.
	match := devtunnel.MatchPort(detail.Resource, published, protocol)
	if match != nil && !errors.Is(match, devtunnel.ErrNoPorts) {
		return match
	}
	for _, remote := range detail.Resource.Ports {
		if remote.Number == published {
			continue
		}
		if err := check(); err != nil {
			return err
		}
		if err := b.dev.DeletePort(ctx, id, remote.Number); err != nil {
			return fmt.Errorf("delete extra public port %d: %w", remote.Number, err)
		}
	}
	if errors.Is(match, devtunnel.ErrNoPorts) {
		if err := check(); err != nil {
			return err
		}
		if err := b.dev.EnsurePort(ctx, id, published, protocol); err != nil {
			return err
		}
	}
	if err := check(); err != nil {
		return err
	}
	verified := b.dev.GetResource(ctx, id)
	if verified.State != devtunnel.ResourceReady {
		return devTunnelResourceError(verified)
	}
	if !verified.Resource.PortsKnown || len(verified.Resource.Ports) != 1 || verified.Resource.Ports[0].Number != published {
		return fmt.Errorf("remote ports do not match sole public port %d after cleanup: %v", published, verified.Resource.Ports)
	}
	if err := devtunnel.MatchPort(verified.Resource, published, protocol); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	_, err = b.storage.Update(func(config *conf.Config) error {
		if err := ensureDevTunnelIDAvailable(config.Tunnels, selfID, model.TunnelPayload{Provider: model.ProviderDevTunnel, DevTunnel: &model.DevTunnelConfig{TunnelID: id}}); err != nil {
			return err
		}
		for i := range config.Tunnels {
			if config.Tunnels[i].ID == selfID {
				target := config.Tunnels[i].DevTunnel
				if target == nil || !devtunnel.SameTunnelID(target.TunnelID, id) || config.Tunnels[i].LocalPort != port || config.Tunnels[i].LocalHost != cfg.LocalHost {
					return fmt.Errorf("service configuration changed during startup")
				}
				target.TunnelID = id
				target.TunnelPort = published
				target.LastPublicURL = ""
				return nil
			}
		}
		return ErrTunnelNotFound
	})
	if err != nil {
		return err
	}
	cfg.TunnelID = id
	cfg.TunnelPort = published
	cfg.LastPublicURL = ""
	b.forgetURL(selfID)
	b.updateDevResourceCache(verified.Resource, false)
	return nil
}

func (b *TunnelBiz) startCanceled(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.starting[id]
	return !ok
}

func (b *TunnelBiz) checkDevTunnelOwner(selfID int, id string) error {
	config, err := b.storage.Load()
	if err != nil {
		return err
	}
	return ensureDevTunnelIDAvailable(config.Tunnels, selfID, model.TunnelPayload{Provider: model.ProviderDevTunnel, DevTunnel: &model.DevTunnelConfig{TunnelID: id}})
}

func (b *TunnelBiz) persistDevTunnelID(id int, tunnelID, account string) error {
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID != id {
				continue
			}
			if cfg.Tunnels[i].DevTunnel == nil {
				cfg.Tunnels[i].DevTunnel = &model.DevTunnelConfig{}
			}
			cfg.Tunnels[i].DevTunnel.TunnelID = tunnelID
			// The pending create request was fulfilled by this id.
			cfg.Tunnels[i].DevTunnel.TunnelName = ""
			if account != "" {
				cfg.Tunnels[i].DevTunnel.Account = account
			}
			return nil
		}
		return ErrTunnelNotFound
	})
	return err
}

func (b *TunnelBiz) rememberURL(id int, url string) {
	url = strings.TrimSpace(url)
	if id <= 0 || url == "" {
		return
	}
	b.mu.Lock()
	b.knownURL[id] = url
	b.mu.Unlock()
}

func (b *TunnelBiz) forgetURL(id int) {
	b.mu.Lock()
	delete(b.knownURL, id)
	b.mu.Unlock()
}

func ensureDevTunnelIDAvailable(tunnels []model.Tunnel, selfID int, payload model.TunnelPayload) error {
	if payload.Provider != model.ProviderDevTunnel || payload.DevTunnel == nil {
		return nil
	}
	id := strings.TrimSpace(payload.DevTunnel.TunnelID)
	name := strings.ToLower(strings.TrimSpace(payload.DevTunnel.TunnelName))
	if id == "" && name == "" {
		return nil
	}
	var owners []string
	for _, item := range tunnels {
		if item.ID == selfID || item.ProviderName() != model.ProviderDevTunnel || item.DevTunnel == nil {
			continue
		}
		otherID := strings.TrimSpace(item.DevTunnel.TunnelID)
		otherName := strings.ToLower(strings.TrimSpace(item.DevTunnel.TunnelName))
		taken := false
		if id != "" {
			taken = devtunnel.SameTunnelID(otherID, id) || (otherName != "" && devtunnel.SameTunnelID(otherName, id))
		}
		if !taken && name != "" {
			taken = devtunnel.SameTunnelID(otherID, name) || (otherName != "" && devtunnel.SameTunnelID(otherName, name))
		}
		if taken {
			owners = append(owners, item.Name)
		}
	}
	if len(owners) > 0 {
		requested := id
		if requested == "" {
			requested = name
		}
		return fmt.Errorf("Tunnel ID %s is already used by %s; remove extra local records without deleting remote resources, then create a separate Tunnel ID", requested, strings.Join(owners, ", "))
	}
	return nil
}

func devTunnelResourceError(detail devtunnel.ResourceDetail) error {
	switch detail.State {
	case devtunnel.ResourceAuth:
		return fmt.Errorf("dev tunnel login expired")
	case devtunnel.ResourceNotFound:
		return fmt.Errorf("dev tunnel was not found")
	case devtunnel.ResourceUnavailable:
		return fmt.Errorf("dev tunnel cli is not installed")
	case devtunnel.ResourcePermission:
		return fmt.Errorf("dev tunnel permission denied")
	case devtunnel.ResourceNetwork:
		if detail.Detail != "" {
			return fmt.Errorf("dev tunnel query failed: %s", detail.Detail)
		}
		return fmt.Errorf("dev tunnel query failed")
	default:
		if detail.Detail != "" {
			return fmt.Errorf("dev tunnel query failed: %s", detail.Detail)
		}
		return fmt.Errorf("dev tunnel query failed")
	}
}

// DeleteDevTunnelResource removes a Microsoft tunnel that no local tunnel still references.
func (b *TunnelBiz) DeleteDevTunnelResource(ctx context.Context, id string) devtunnel.DeleteResourceResult {
	id = strings.TrimSpace(id)
	if !devtunnel.ValidTunnelID(id) {
		return devtunnel.DeleteResourceResult{State: devtunnel.ResourceError, Detail: "invalid dev tunnel id"}
	}
	cfg, err := b.storage.Load()
	if err != nil {
		return devtunnel.DeleteResourceResult{State: devtunnel.ResourceError, Detail: err.Error()}
	}
	for _, item := range cfg.Tunnels {
		if item.ProviderName() != model.ProviderDevTunnel || item.DevTunnel == nil {
			continue
		}
		if !devtunnel.SameTunnelID(item.DevTunnel.TunnelID, id) {
			continue
		}
		if b.isRunning(item.ID) {
			return devtunnel.DeleteResourceResult{State: "running", Detail: "stop the tunnel and remove the binding first", TunnelName: item.Name}
		}
		return devtunnel.DeleteResourceResult{State: "linked", Detail: "remove the local tunnel binding first", TunnelName: item.Name}
	}
	detail := b.dev.GetResource(ctx, id)
	if detail.State != devtunnel.ResourceReady {
		return devtunnel.DeleteResourceResult{State: detail.State, Detail: detail.Detail}
	}
	target := detail.Resource.ID
	if !devtunnel.ValidTunnelID(target) {
		target = id
	}
	if err := b.dev.DeleteRemote(ctx, target); err != nil {
		state, message := devTunnelFailureKind(err)
		return devtunnel.DeleteResourceResult{State: state, Detail: message}
	}
	b.updateDevResourceCache(devtunnel.Resource{ID: target}, true)
	return devtunnel.DeleteResourceResult{State: "deleted"}
}

func devTunnelFailureKind(err error) (string, string) {
	text := strings.TrimSpace(err.Error())
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "login"), strings.Contains(lower, "not signed"):
		return devtunnel.ResourceAuth, text
	case strings.Contains(lower, "not found"):
		return devtunnel.ResourceNotFound, text
	case strings.Contains(lower, "permission"), strings.Contains(lower, "forbidden"):
		return devtunnel.ResourcePermission, text
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "network"):
		return devtunnel.ResourceNetwork, text
	default:
		return devtunnel.ResourceError, text
	}
}

func testDevTunnelLocal(payload model.TunnelPayload) (time.Duration, error) {
	if err := validateTunnelPayload(payload); err != nil {
		return 0, err
	}
	started := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(payload.LocalHost, strconv.Itoa(payload.LocalPort)), 3*time.Second)
	if err != nil {
		return 0, fmt.Errorf("target service is not accepting connections: %w", err)
	}
	_ = conn.Close()
	return time.Since(started), nil
}

func validateDevTunnelPayload(payload model.TunnelPayload) error {
	if !devtunnel.ValidTargetHost(payload.LocalHost) {
		return fmt.Errorf("invalid target host: expected an IPv4 address, IPv6 address, or hostname")
	}
	switch payload.Mode {
	case "http", "https", "auto":
	default:
		return fmt.Errorf("unsupported dev tunnel protocol: %s", payload.Mode)
	}
	access := "private"
	if payload.DevTunnel != nil && payload.DevTunnel.Access != "" {
		access = payload.DevTunnel.Access
	}
	if access != "private" && access != "public" && !(access == "unknown" && payload.DevTunnelIDMode == "bind") {
		return fmt.Errorf("unsupported dev tunnel access: %s", access)
	}
	expiration := ""
	if payload.DevTunnel != nil {
		expiration = payload.DevTunnel.Expiration
	}
	if _, err := devtunnel.NormalizeExpiration(expiration); err != nil {
		return err
	}
	region := ""
	if payload.DevTunnel != nil {
		region = payload.DevTunnel.Region
	}
	if _, err := devtunnel.NormalizeRegion(region); err != nil {
		return err
	}
	switch payload.DevTunnelIDMode {
	case "", "auto", "custom", "bind":
	default:
		return fmt.Errorf("unsupported dev tunnel id mode")
	}
	if payload.DevTunnelIDMode == "bind" {
		if payload.DevTunnel == nil || strings.TrimSpace(payload.DevTunnel.TunnelID) == "" {
			return fmt.Errorf("dev tunnel id is required")
		}
	}
	if payload.DevTunnelIDMode == "custom" {
		if payload.DevTunnel == nil {
			return fmt.Errorf("dev tunnel id is required")
		}
		if strings.TrimSpace(payload.DevTunnel.TunnelID) != "" {
			return fmt.Errorf("a custom dev tunnel id cannot be combined with an existing tunnel id")
		}
		if _, err := devtunnel.NormalizeTunnelName(payload.DevTunnel.TunnelName); err != nil {
			return err
		}
	}
	if payload.DevTunnel != nil && payload.DevTunnel.TunnelID != "" && !devtunnel.ValidTunnelID(payload.DevTunnel.TunnelID) {
		return fmt.Errorf("invalid dev tunnel id")
	}
	if payload.DevTunnel != nil && payload.DevTunnel.TunnelName != "" {
		if _, err := devtunnel.NormalizeTunnelName(payload.DevTunnel.TunnelName); err != nil {
			return err
		}
	}
	return nil
}

func tunnelFromPayload(id int, payload model.TunnelPayload, previous *model.Tunnel) model.Tunnel {
	item := model.Tunnel{
		ID:          id,
		Name:        payload.Name,
		Provider:    payload.Provider,
		GroupID:     payload.GroupID,
		Mode:        payload.Mode,
		JumperIDs:   append([]int{}, payload.JumperIDs...),
		LocalHost:   payload.LocalHost,
		LocalPort:   payload.LocalPort,
		RemoteHost:  payload.RemoteHost,
		RemotePort:  payload.RemotePort,
		AutoStart:   payload.AutoStart,
		Status:      payload.Status,
		Description: payload.Description,
	}
	if previous != nil {
		item.ManuallyStopped = previous.ManuallyStopped
		item.SyncID = previous.SyncID
		item.LastError = previous.LastError
	}
	switch payload.Provider {
	case model.ProviderFRP:
		cfg := *payload.FRP
		item.FRP = &cfg
		item.JumperIDs = []int{}
		return item
	case model.ProviderDevTunnel:
		item.JumperIDs = []int{}
		item.RemoteHost = ""
		item.RemotePort = 0
		item.Cloudflare = nil
		cfg := model.DevTunnelConfig{Access: "private", Expiration: "30d", Protocol: payload.Mode}
		if payload.DevTunnel != nil {
			cfg = *payload.DevTunnel
		}
		cfg.LocalHost = payload.LocalHost
		cfg.LocalPort = payload.LocalPort
		cfg.Protocol = payload.Mode
		if cfg.Access == "" {
			cfg.Access = "private"
		}
		if cfg.Expiration == "" {
			cfg.Expiration = "30d"
		}
		switch payload.DevTunnelIDMode {
		case "auto":
			cfg.TunnelID = ""
			cfg.TunnelName = ""
		case "bind":
			cfg.TunnelID = strings.TrimSpace(cfg.TunnelID)
			cfg.TunnelName = ""
		case "custom":
			name, nameErr := devtunnel.NormalizeTunnelName(cfg.TunnelName)
			if nameErr != nil {
				name = strings.ToLower(strings.TrimSpace(cfg.TunnelName))
			}
			cfg.TunnelName = name
			cfg.TunnelID = ""
		default:
			if previous != nil && previous.DevTunnel != nil && cfg.TunnelID == "" {
				cfg.TunnelID = previous.DevTunnel.TunnelID
			}
			cfg.TunnelName = strings.ToLower(strings.TrimSpace(cfg.TunnelName))
			if cfg.TunnelID != "" {
				// An inherited existing id replaces any pending create request.
				cfg.TunnelName = ""
			}
		}
		cfg.LastPublicURL = ""
		cfg.TunnelPort = 0
		if previous != nil && previous.DevTunnel != nil && previous.LocalPort == payload.LocalPort && devtunnel.SameTunnelID(previous.DevTunnel.TunnelID, cfg.TunnelID) {
			cfg.LastPublicURL = previous.DevTunnel.LastPublicURL
			cfg.TunnelPort = previous.DevTunnel.TunnelPort
		}
		item.DevTunnel = &cfg
		return item
	case model.ProviderCloudflare:
		item.JumperIDs = []int{}
		item.RemoteHost = ""
		item.RemotePort = 0
		item.DevTunnel = nil
		cfg := model.CloudflareTunnelConfig{Protocol: payload.Mode}
		if payload.Cloudflare != nil {
			cfg = *payload.Cloudflare
		}
		cfg.LocalHost = payload.LocalHost
		cfg.LocalPort = payload.LocalPort
		cfg.Protocol = payload.Mode
		cfg.TunnelName = strings.TrimSpace(cfg.TunnelName)
		cfg.TunnelID = strings.TrimSpace(cfg.TunnelID)
		if host, err := cloudflared.NormalizeHostname(cfg.Hostname); err == nil {
			cfg.Hostname = host
		}
		switch payload.CloudflareIDMode {
		case "auto":
			cfg.TunnelID = ""
		case "bind":
			cfg.TunnelID = strings.TrimSpace(cfg.TunnelID)
		default:
			if previous != nil && previous.Cloudflare != nil && cfg.TunnelID == "" {
				cfg.TunnelID = previous.Cloudflare.TunnelID
			}
		}
		item.Cloudflare = &cfg
		return item
	default:
		item.Provider = model.ProviderSSH
		item.DevTunnel = nil
		item.Cloudflare = nil
		return item
	}
}
