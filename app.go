package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/energye/systray"
	"loris-tunnel/internal/aidebug"
	"loris-tunnel/internal/autostart"
	"loris-tunnel/internal/biz"
	"loris-tunnel/internal/cloudflared"
	"loris-tunnel/internal/cloudsync"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/device"
	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/license"
	"loris-tunnel/internal/model"
	"loris-tunnel/internal/sshconfig"
	"loris-tunnel/internal/traytext"
	"loris-tunnel/internal/uilocale"
	"loris-tunnel/internal/updater"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const usageHeartbeatInterval = 2 * time.Hour
const (
	aiReportSupportEmail = "admin@lorisdev.cc"
	maxMailtoSubjectLen  = 180
	maxMailtoBodyLen     = 3500
)

type OpenReportEmailPayload struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type OpenReportEmailResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// App struct
type App struct {
	configGate    sync.Mutex
	cloudSync     *cloudsync.Service
	cloudSyncErr  error
	ctx           context.Context
	storage       *conf.Storage
	jumper        *biz.JumperBiz
	group         *biz.GroupBiz
	tunnel        *biz.TunnelBiz
	updater       *updater.Service
	license       *license.Client
	licenseActive atomic.Bool
	aiDebug       *aidebug.Service
	machineID     string
	initErr       error

	trayMu   sync.Mutex
	trayShow *systray.MenuItem
	trayQuit *systray.MenuItem

	allowClose atomic.Bool

	usageReporterStop chan struct{}
	usageReporterWG   sync.WaitGroup

	trafficMu       sync.Mutex
	lastTrafficUp   uint64
	lastTrafficDown uint64
	lastTrafficAt   time.Time
}

// NewApp creates a new App application struct
func NewApp() *App {
	storage, err := conf.NewDefaultStorage()
	if err != nil {
		return &App{initErr: err}
	}
	level := detectLogLevel()
	if err := configureLogger(storage.Path(), level); err != nil {
		fmt.Printf("logger init failed: %v\n", err)
	}
	slog.Info("app initialized", "config", storage.Path())

	licenseClient := license.NewDefaultClient()
	machineID := device.MachineID()
	return &App{
		storage:   storage,
		jumper:    biz.NewJumperBiz(storage),
		group:     biz.NewGroupBiz(storage),
		tunnel:    biz.NewTunnelBiz(storage),
		updater:   newUpdaterService(),
		license:   licenseClient,
		aiDebug:   aidebug.NewService(licenseClient.BaseURL(), machineID),
		machineID: machineID,
	}
}

func newUpdaterService() *updater.Service {
	if strings.EqualFold(strings.TrimSpace(distributionChannel), "store") {
		return nil
	}
	return updater.NewDefaultService()
}

// GetDistributionChannel reports the release channel to the frontend.
func (a *App) GetDistributionChannel() string {
	if strings.EqualFold(strings.TrimSpace(distributionChannel), "store") {
		return "store"
	}
	return "github"
}

func detectLogLevel() slog.Level {
	// Explicit override takes highest priority.
	if raw := strings.TrimSpace(os.Getenv("LORIS_TUNNEL_LOG_LEVEL")); raw != "" {
		return parseLogLevel(raw)
	}
	// `wails dev` injects `devserver`; use debug logging for dev runtime.
	if strings.TrimSpace(os.Getenv("devserver")) != "" {
		return slog.LevelDebug
	}
	// Built binary defaults to info.
	return slog.LevelInfo
}

func parseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func configureLogger(configPath string, level slog.Level) error {
	dir := filepath.Dir(strings.TrimSpace(configPath))
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create log dir failed: %w", err)
	}

	logPath := filepath.Join(dir, "loris-tunnel.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file failed: %w", err)
	}

	handler := slog.NewTextHandler(file, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
	slog.Info("logger initialized", "path", logPath, "level", level.String())
	return nil
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
// SetTrayMenuItems wires systray menu entries created in main so locale changes can relabel them.
func (a *App) SetTrayMenuItems(show, quit *systray.MenuItem) {
	a.trayMu.Lock()
	defer a.trayMu.Unlock()
	a.trayShow = show
	a.trayQuit = quit
}

func (a *App) applyTrayLocaleUnlocked(tag string) {
	s := traytext.ForLocale(tag)
	if a.trayShow != nil {
		a.trayShow.SetTitle(s.ShowMainTitle)
		a.trayShow.SetTooltip(s.ShowMainTooltip)
	}
	if a.trayQuit != nil {
		a.trayQuit.SetTitle(s.QuitTitle)
		a.trayQuit.SetTooltip(s.QuitTooltip)
	}
	if runtime.GOOS != "darwin" {
		systray.SetTitle(s.AppTitle)
	}
	systray.SetTooltip(s.IconTooltip)
}

// ApplyTrayLocale updates tray icon tooltip and menu item titles to match a vue-i18n locale tag.
func (a *App) ApplyTrayLocale(locale string) {
	a.trayMu.Lock()
	defer a.trayMu.Unlock()
	tag := uilocale.Normalize(locale)
	if tag == "" {
		tag = "en"
	}
	a.applyTrayLocaleUnlocked(tag)
}

// SaveUILocale persists ui.locale beside config.toml and refreshes the tray (call when the UI language changes).
func (a *App) SaveUILocale(locale string) error {
	if a.storage == nil {
		return fmt.Errorf("storage unavailable")
	}
	dir := filepath.Dir(a.storage.Path())
	if err := uilocale.WriteFile(dir, locale); err != nil {
		return err
	}
	a.trayMu.Lock()
	defer a.trayMu.Unlock()
	tag := uilocale.Normalize(locale)
	if tag == "" {
		tag = "en"
	}
	a.applyTrayLocaleUnlocked(tag)
	return nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	buildType := wailsruntime.Environment(ctx).BuildType
	a.license = license.NewClientByBuildType(buildType)
	a.aiDebug = aidebug.NewService(a.license.BaseURL(), a.machineID)
	slog.Info("license client initialized", "build_type", buildType, "api_base_url", a.license.BaseURL())
	slog.Info("app startup")
	if err := a.ensureReady(); err == nil {
		a.syncAutoRunWithConfig()
		a.cloudSyncErr = a.initializeCloudSync()
		if a.cloudSync != nil {
			a.cloudSync.Start()
		}
		go func() {
			limit := a.tunnelStartLimit()
			a.configGate.Lock()
			defer a.configGate.Unlock()
			if a.allowClose.Load() {
				return
			}
			if err := a.tunnel.StartAutoStart(limit); err != nil {
				slog.Error("auto start tunnel failed", "err", err)
			}
		}()
		a.startUsageReporter()
	}
}

func (a *App) PrepareForQuit() {
	a.allowClose.Store(true)
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if runtime.GOOS != "windows" {
		return false
	}
	if a.allowClose.Load() {
		return false
	}
	slog.Info("window close intercepted; hiding to tray")
	wailsruntime.Hide(ctx)
	wailsruntime.WindowHide(ctx)
	return true
}

func (a *App) shutdown(ctx context.Context) {
	_ = ctx
	a.allowClose.Store(true)
	slog.Info("app shutdown")
	if a.cloudSync != nil {
		a.cloudSync.Close()
	}
	a.stopUsageReporter()
	if a.tunnel != nil {
		a.configGate.Lock()
		a.tunnel.Shutdown()
		a.configGate.Unlock()
	}
}

func (a *App) startUsageReporter() {
	if a.license == nil || strings.TrimSpace(a.machineID) == "" {
		slog.Warn("usage reporter skipped: missing client or machine id")
		return
	}
	if a.usageReporterStop != nil {
		return
	}
	a.usageReporterStop = make(chan struct{})
	a.usageReporterWG.Add(1)
	go func() {
		defer a.usageReporterWG.Done()

		a.reportUsageEvent("startup")

		ticker := time.NewTicker(usageHeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				a.reportUsageEvent("heartbeat")
			case <-a.usageReporterStop:
				return
			}
		}
	}()
}

func (a *App) stopUsageReporter() {
	if a.usageReporterStop == nil {
		return
	}
	close(a.usageReporterStop)
	a.usageReporterStop = nil
	a.usageReporterWG.Wait()
}

func (a *App) usageAppVersion() string {
	if a.license != nil && a.license.Environment() == "dev" {
		return productVersion + "-dev"
	}
	return productVersion
}

func (a *App) reportUsageEvent(eventType string) {
	if a.license == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := a.license.ReportUsageEvent(ctx, model.UsageEventRequest{
		MachineID:  a.machineID,
		EventType:  eventType,
		AppVersion: a.usageAppVersion(),
		Platform:   runtime.GOOS,
		ClientTS:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		slog.Warn("report usage event failed", "event_type", eventType, "error", err)
	}
}

func (a *App) ensureReady() error {
	if a.initErr != nil {
		return a.initErr
	}
	if a.storage == nil || a.jumper == nil || a.group == nil || a.tunnel == nil {
		return fmt.Errorf("app is not initialized")
	}
	return nil
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

func (a *App) GetState() (model.State, error) {
	if err := a.ensureReady(); err != nil {
		return model.State{}, err
	}
	cfg, err := a.storage.Load()
	if err != nil {
		return model.State{}, err
	}
	a.tunnel.AttachRuntimeState(cfg.Tunnels)
	return model.State{
		Jumpers:         append([]model.Jumper{}, cfg.Jumpers...),
		Groups:          append([]model.TunnelGroup{}, cfg.Groups...),
		Tunnels:         append([]model.Tunnel{}, cfg.Tunnels...),
		ActiveSlots:     a.tunnel.RunningCount(),
		ActiveSlotLimit: a.currentTunnelStartLimit(),
	}, nil
}

func (a *App) GetTrafficStats() (model.TrafficStats, error) {
	if err := a.ensureReady(); err != nil {
		return model.TrafficStats{}, err
	}

	up, down := a.tunnel.TrafficSnapshot()
	now := time.Now()

	a.trafficMu.Lock()
	defer a.trafficMu.Unlock()

	var upBps, downBps int64
	if !a.lastTrafficAt.IsZero() {
		elapsed := now.Sub(a.lastTrafficAt).Seconds()
		if elapsed > 0 {
			upBps = int64(float64(up-a.lastTrafficUp) / elapsed)
			downBps = int64(float64(down-a.lastTrafficDown) / elapsed)
			if upBps < 0 {
				upBps = 0
			}
			if downBps < 0 {
				downBps = 0
			}
		}
	}

	a.lastTrafficUp = up
	a.lastTrafficDown = down
	a.lastTrafficAt = now

	return model.TrafficStats{
		UpBps:   upBps,
		DownBps: downBps,
	}, nil
}

func (a *App) ListJumpers() ([]model.Jumper, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.jumper.List()
}

func (a *App) GetSSHConfigImportSources() ([]model.SSHConfigImportSource, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return sshconfig.GetImportSources()
}

func (a *App) LoadSSHConfigJumpersByPath(configPath string) (model.SSHConfigImportResult, error) {
	if err := a.ensureReady(); err != nil {
		return model.SSHConfigImportResult{}, err
	}
	return sshconfig.LoadImportCandidates(configPath)
}

func (a *App) CreateJumper(payload model.JumperPayload) (model.Jumper, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return model.Jumper{}, err
	}
	return a.jumper.Create(payload)
}

func (a *App) UpdateJumper(id int, payload model.JumperPayload) (model.Jumper, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return model.Jumper{}, err
	}
	return a.jumper.Update(id, payload)
}

func (a *App) TestJumperConnection(payload model.JumperPayload) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.jumper.TestConnection(payload)
}

func (a *App) DeleteJumper(id int) error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.jumper.Delete(id)
}

func (a *App) ListTunnels() ([]model.Tunnel, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.tunnel.List()
}

func (a *App) ListGroups() ([]model.TunnelGroup, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.group.List()
}

func (a *App) CreateGroup(payload model.TunnelGroupPayload) (model.TunnelGroup, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return model.TunnelGroup{}, err
	}
	return a.group.Create(payload)
}

func (a *App) UpdateGroup(id int, payload model.TunnelGroupPayload) (model.TunnelGroup, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return model.TunnelGroup{}, err
	}
	return a.group.Update(id, payload)
}

func (a *App) DeleteGroup(id int) error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.group.Delete(id)
}

func (a *App) ReorderGroups(ids []int) error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.group.Reorder(ids)
}

func (a *App) CreateTunnel(payload model.TunnelPayload) (model.Tunnel, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return model.Tunnel{}, err
	}
	return a.tunnel.Create(payload)
}

func (a *App) UpdateTunnel(id int, payload model.TunnelPayload) (model.Tunnel, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return model.Tunnel{}, err
	}
	return a.tunnel.Update(id, payload)
}

func (a *App) MoveTunnelToGroup(id int, groupID int) (model.Tunnel, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return model.Tunnel{}, err
	}
	return a.tunnel.MoveToGroup(id, groupID)
}

func (a *App) TestTunnelConnection(payload model.TunnelPayload, inlineJumper *model.JumperPayload) (model.TunnelConnectionTestResult, error) {
	if err := a.ensureReady(); err != nil {
		return model.TunnelConnectionTestResult{}, err
	}
	latency, err := a.tunnel.TestConnection(payload, inlineJumper)
	if err != nil {
		return model.TunnelConnectionTestResult{}, err
	}
	return model.TunnelConnectionTestResult{
		LatencyMs: latency.Milliseconds(),
	}, nil
}

func (a *App) DebugJumperFailure(payload model.JumperPayload, rawError string, uiLocale string) (model.AIDebugResult, error) {
	if err := a.ensureReady(); err != nil {
		return model.AIDebugResult{}, err
	}
	if a.aiDebug == nil {
		return model.AIDebugResult{}, fmt.Errorf("ai debug service is not initialized")
	}

	jumper := model.Jumper{
		Name:                   strings.TrimSpace(payload.Name),
		Host:                   strings.TrimSpace(payload.Host),
		Port:                   payload.Port,
		User:                   strings.TrimSpace(payload.User),
		AuthType:               strings.TrimSpace(payload.AuthType),
		KeyPath:                strings.TrimSpace(payload.KeyPath),
		AgentSocketPath:        strings.TrimSpace(payload.AgentSocketPath),
		BypassHostVerification: payload.BypassHostVerification,
		KeepAliveIntervalMs:    payload.KeepAliveIntervalMs,
		TimeoutMs:              payload.TimeoutMs,
		HostKeyAlgorithms:      strings.TrimSpace(payload.HostKeyAlgorithms),
		Notes:                  strings.TrimSpace(payload.Notes),
	}

	return a.aiDebug.Diagnose(context.Background(), aidebug.DiagnosticInput{
		TargetType:  "jumper_test",
		RawError:    rawError,
		UILocale:    uiLocale,
		JumperChain: []model.Jumper{jumper},
	})
}

func (a *App) DebugTunnelFailure(payload model.TunnelPayload, inlineJumper *model.JumperPayload, rawError string, uiLocale string) (model.AIDebugResult, error) {
	if err := a.ensureReady(); err != nil {
		return model.AIDebugResult{}, err
	}
	if a.aiDebug == nil {
		return model.AIDebugResult{}, fmt.Errorf("ai debug service is not initialized")
	}

	chain := make([]model.Jumper, 0, len(payload.JumperIDs)+1)
	if len(payload.JumperIDs) > 0 {
		cfg, err := a.storage.Load()
		if err != nil {
			return model.AIDebugResult{}, err
		}
		jumpers, err := collectJumpersForApp(cfg.Jumpers, payload.JumperIDs)
		if err != nil {
			return model.AIDebugResult{}, err
		}
		chain = append(chain, jumpers...)
	}
	if inlineJumper != nil {
		chain = append(chain, model.Jumper{
			Name:                   strings.TrimSpace(inlineJumper.Name),
			Host:                   strings.TrimSpace(inlineJumper.Host),
			Port:                   inlineJumper.Port,
			User:                   strings.TrimSpace(inlineJumper.User),
			AuthType:               strings.TrimSpace(inlineJumper.AuthType),
			KeyPath:                strings.TrimSpace(inlineJumper.KeyPath),
			AgentSocketPath:        strings.TrimSpace(inlineJumper.AgentSocketPath),
			BypassHostVerification: inlineJumper.BypassHostVerification,
			KeepAliveIntervalMs:    inlineJumper.KeepAliveIntervalMs,
			TimeoutMs:              inlineJumper.TimeoutMs,
			HostKeyAlgorithms:      strings.TrimSpace(inlineJumper.HostKeyAlgorithms),
			Notes:                  strings.TrimSpace(inlineJumper.Notes),
		})
	}

	tunnel := model.Tunnel{
		Name:        strings.TrimSpace(payload.Name),
		Mode:        strings.TrimSpace(payload.Mode),
		JumperIDs:   append([]int{}, payload.JumperIDs...),
		LocalHost:   strings.TrimSpace(payload.LocalHost),
		LocalPort:   payload.LocalPort,
		RemoteHost:  strings.TrimSpace(payload.RemoteHost),
		RemotePort:  payload.RemotePort,
		AutoStart:   payload.AutoStart,
		Status:      strings.TrimSpace(payload.Status),
		Description: strings.TrimSpace(payload.Description),
	}

	return a.aiDebug.Diagnose(context.Background(), aidebug.DiagnosticInput{
		TargetType:  "tunnel_test",
		RawError:    rawError,
		UILocale:    uiLocale,
		Tunnel:      &tunnel,
		JumperChain: chain,
	})
}

func (a *App) DebugSavedTunnelFailure(id int, rawError string, uiLocale string) (model.AIDebugResult, error) {
	if err := a.ensureReady(); err != nil {
		return model.AIDebugResult{}, err
	}
	if a.aiDebug == nil {
		return model.AIDebugResult{}, fmt.Errorf("ai debug service is not initialized")
	}
	if id <= 0 {
		return model.AIDebugResult{}, fmt.Errorf("invalid tunnel id")
	}

	cfg, err := a.storage.Load()
	if err != nil {
		return model.AIDebugResult{}, err
	}

	var tunnel model.Tunnel
	found := false
	for _, item := range cfg.Tunnels {
		if item.ID == id {
			tunnel = item
			found = true
			break
		}
	}
	if !found {
		return model.AIDebugResult{}, biz.ErrTunnelNotFound
	}

	jumpers, err := collectJumpersForApp(cfg.Jumpers, tunnel.JumperIDs)
	if err != nil {
		return model.AIDebugResult{}, err
	}

	if strings.TrimSpace(rawError) == "" {
		rawError = tunnel.LastError
	}

	return a.aiDebug.Diagnose(context.Background(), aidebug.DiagnosticInput{
		TargetType:  "tunnel_runtime_error",
		RawError:    rawError,
		UILocale:    uiLocale,
		Tunnel:      &tunnel,
		JumperChain: jumpers,
	})
}

func (a *App) DeleteTunnel(id int) error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.tunnel.Delete(id)
}

func (a *App) PreviewTunnelDeletion(id int) (biz.DeletionPreview, error) {
	if err := a.ensureReady(); err != nil {
		return biz.DeletionPreview{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.PreviewTunnelDeletion(ctx, id), nil
}

func (a *App) DeleteTunnelWithOptions(id int, cleanupRemote bool) (biz.DeletionResult, error) {
	if err := a.ensureReady(); err != nil {
		return biz.DeletionResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.tunnel.DeleteTunnelWithOptions(ctx, id, cleanupRemote), nil
}

func (a *App) PreviewProviderResourceDeletion(provider, tunnelID string) (biz.DeletionPreview, error) {
	if err := a.ensureReady(); err != nil {
		return biz.DeletionPreview{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.PreviewProviderResourceDeletion(ctx, provider, tunnelID), nil
}

func (a *App) DeleteProviderResourceWithOptions(provider, tunnelID string, cleanupDNS bool) (biz.DeletionResult, error) {
	if err := a.ensureReady(); err != nil {
		return biz.DeletionResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.tunnel.DeleteProviderResourceWithOptions(ctx, provider, tunnelID, cleanupDNS), nil
}

func (a *App) RetryDeletionCleanup(operationID string) (biz.DeletionResult, error) {
	if err := a.ensureReady(); err != nil {
		return biz.DeletionResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.tunnel.RetryDeletionCleanup(ctx, operationID), nil
}

func (a *App) ListDeletionTasks() ([]biz.DeletionResult, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.tunnel.ListDeletionTasks(), nil
}

func (a *App) ToggleTunnel(id int) (model.Tunnel, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return model.Tunnel{}, err
	}
	return a.tunnel.Toggle(id, a.tunnelStartLimit)
}

func (a *App) GetDevTunnelStatus() (devtunnel.Status, error) {
	if err := a.ensureReady(); err != nil {
		return devtunnel.Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.DevTunnelStatus(ctx)
}

func (a *App) ListDevTunnelResources(force bool) (devtunnel.ResourceList, error) {
	if err := a.ensureReady(); err != nil {
		return devtunnel.ResourceList{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.ListDevTunnelResourcesCached(ctx, force), nil
}

func (a *App) LoadDevTunnelRegions(measure, force bool) (devtunnel.RegionProbe, error) {
	if err := a.ensureReady(); err != nil {
		return devtunnel.RegionProbe{}, err
	}
	timeout := 15 * time.Second
	if measure {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return a.tunnel.LoadDevTunnelRegions(ctx, measure, force), nil
}

func (a *App) GetDevTunnelResource(id string, force bool) (devtunnel.ResourceDetail, error) {
	if err := a.ensureReady(); err != nil {
		return devtunnel.ResourceDetail{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.GetDevTunnelResourceCached(ctx, id, force), nil
}

func (a *App) CreateDevTunnelResource(input devtunnel.CreateResourceInput) (devtunnel.ResourceDetail, error) {
	if err := a.ensureReady(); err != nil {
		return devtunnel.ResourceDetail{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.tunnel.CreateDevTunnelResource(ctx, input), nil
}

func (a *App) DeleteDevTunnelResource(id string) (devtunnel.DeleteResourceResult, error) {
	if err := a.ensureReady(); err != nil {
		return devtunnel.DeleteResourceResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.tunnel.DeleteDevTunnelResource(ctx, id), nil
}

func (a *App) StartDevTunnelLogin(kind string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.tunnel.StartDevTunnelLogin(kind, func(phase, loginURL string) {
		if a.ctx == nil {
			return
		}
		wailsruntime.EventsEmit(a.ctx, "devtunnel:login-progress", map[string]string{
			"phase": phase,
			"url":   loginURL,
		})
	}, func() {
		if a.ctx == nil {
			return
		}
		wailsruntime.EventsEmit(a.ctx, "devtunnel:login")
	})
}

func (a *App) CancelDevTunnelLogin() {
	if a.tunnel == nil {
		return
	}
	a.tunnel.CancelDevTunnelLogin()
}

func (a *App) LogoutDevTunnel() error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.LogoutDevTunnel(ctx)
}

func (a *App) InstallDevTunnelCLI() error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return a.tunnel.InstallDevTunnelCLI(ctx)
}

func (a *App) CancelDevTunnelInstall() {
	if a.tunnel == nil {
		return
	}
	a.tunnel.CancelDevTunnelInstall()
}

func (a *App) GetCloudflareStatus() (cloudflared.Status, error) {
	if err := a.ensureReady(); err != nil {
		return cloudflared.Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.CloudflareStatus(ctx)
}

func (a *App) ListCloudflareResources(force bool) (cloudflared.ResourceList, error) {
	if err := a.ensureReady(); err != nil {
		return cloudflared.ResourceList{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.ListCloudflareResourcesCached(ctx, force), nil
}

func (a *App) ListCloudflareZones(force bool) (cloudflared.ZoneList, error) {
	if err := a.ensureReady(); err != nil {
		return cloudflared.ZoneList{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.ListCloudflareAuthorizations(ctx, force), nil
}

func (a *App) ListCloudflareAuthorizations(force bool) (cloudflared.ZoneList, error) {
	return a.ListCloudflareZones(force)
}

func (a *App) RemoveCloudflareAuthorization(zoneID string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.tunnel.RemoveCloudflareAuthorization(ctx, zoneID)
}

func (a *App) GetCloudflareResource(id string, force bool) (cloudflared.ResourceDetail, error) {
	if err := a.ensureReady(); err != nil {
		return cloudflared.ResourceDetail{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.tunnel.GetCloudflareResourceCached(ctx, id, force), nil
}

func (a *App) CreateCloudflareResource(input cloudflared.CreateResourceInput) (cloudflared.ResourceDetail, error) {
	if err := a.ensureReady(); err != nil {
		return cloudflared.ResourceDetail{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.tunnel.CreateCloudflareResource(ctx, input), nil
}

func (a *App) DeleteCloudflareResource(id string) (cloudflared.DeleteResourceResult, error) {
	if err := a.ensureReady(); err != nil {
		return cloudflared.DeleteResourceResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.tunnel.DeleteCloudflareResource(ctx, id), nil
}

func (a *App) StartCloudflareLogin() error {
	return a.StartCloudflareZoneAuthorization("")
}

func (a *App) StartCloudflareZoneAuthorization(zoneID string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.tunnel.StartCloudflareZoneAuthorization(zoneID, func(phase, loginURL string) {
		if a.ctx == nil {
			return
		}
		wailsruntime.EventsEmit(a.ctx, "cloudflare:login-progress", map[string]string{
			"phase": phase,
			"url":   loginURL,
		})
	}, func() {
		if a.ctx == nil {
			return
		}
		wailsruntime.EventsEmit(a.ctx, "cloudflare:login")
	})
}

func (a *App) CancelCloudflareLogin() {
	if a.tunnel == nil {
		return
	}
	a.tunnel.CancelCloudflareLogin()
}

func (a *App) InstallCloudflareCLI() error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return a.tunnel.InstallCloudflareCLI(ctx)
}

func (a *App) CancelCloudflareInstall() {
	if a.tunnel == nil {
		return
	}
	a.tunnel.CancelCloudflareInstall()
}

func (a *App) currentTunnelStartLimit() int {
	if a.licenseActive.Load() {
		return 0
	}
	return biz.FreePlanRunningLimit
}

func (a *App) tunnelStartLimit() int {
	if a.license == nil {
		return biz.FreePlanRunningLimit
	}
	machineID := strings.TrimSpace(a.machineID)
	if machineID == "" {
		machineID = device.MachineID()
	}
	if machineID == "" {
		return biz.FreePlanRunningLimit
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := a.license.GetStatus(ctx, machineID)
	if err != nil {
		slog.Warn("license check for tunnel start limit failed; applying free plan limit", "err", err)
		return biz.FreePlanRunningLimit
	}
	a.licenseActive.Store(status.Active)
	return a.currentTunnelStartLimit()
}

func collectJumpersForApp(items []model.Jumper, ids []int) ([]model.Jumper, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one jumper is required")
	}
	index := make(map[int]model.Jumper, len(items))
	for _, item := range items {
		index[item.ID] = item
	}
	result := make([]model.Jumper, 0, len(ids))
	for _, id := range ids {
		jumper, ok := index[id]
		if !ok {
			return nil, biz.ErrJumperNotFound
		}
		result = append(result, jumper)
	}
	return result, nil
}

func (a *App) GetMachineID() (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.machineID) == "" {
		a.machineID = device.MachineID()
	}
	return a.machineID, nil
}

func (a *App) GetStoredLicenseCode() (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	cfg, err := a.storage.Load()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(cfg.License.Code), nil
}

func (a *App) GetLicenseStatus() (model.LicenseStatus, error) {
	if err := a.ensureReady(); err != nil {
		return model.LicenseStatus{}, err
	}
	if a.license == nil {
		return model.LicenseStatus{}, fmt.Errorf("license service is not initialized")
	}
	machineID, err := a.GetMachineID()
	if err != nil {
		return model.LicenseStatus{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status, err := a.license.GetStatus(ctx, machineID)
	if err != nil {
		return model.LicenseStatus{}, err
	}
	a.licenseActive.Store(status.Active)
	if err := a.saveLicenseCode(status.Code); err != nil {
		slog.Warn("save license code failed", "error", err)
	}
	return status, nil
}

func (a *App) RedeemLicenseCode(code string) (model.LicenseRedeemResult, error) {
	if err := a.ensureReady(); err != nil {
		return model.LicenseRedeemResult{}, err
	}
	if a.license == nil {
		return model.LicenseRedeemResult{}, fmt.Errorf("license service is not initialized")
	}
	machineID, err := a.GetMachineID()
	if err != nil {
		return model.LicenseRedeemResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := a.license.Redeem(ctx, machineID, code)
	if err != nil {
		return model.LicenseRedeemResult{}, err
	}
	a.licenseActive.Store(result.Active)
	if err := a.saveLicenseCode(result.Code); err != nil {
		slog.Warn("save license code failed", "error", err)
	}
	return result, nil
}

func (a *App) CheckForUpdates(currentVersion string) (updater.Result, error) {
	if err := a.ensureReady(); err != nil {
		return updater.Result{}, err
	}
	if a.updater == nil {
		return updater.Result{}, fmt.Errorf("updater service is not initialized")
	}
	return a.updater.Check(context.Background(), currentVersion)
}

func (a *App) saveLicenseCode(code string) error {
	if a.storage == nil {
		return fmt.Errorf("storage is not initialized")
	}
	trimmed := strings.TrimSpace(code)
	_, err := a.storage.Update(func(cfg *conf.Config) error {
		cfg.License.Code = trimmed
		return nil
	})
	return err
}

// syncAutoRunWithConfig aligns OS auto-run (launch at login) state with config.
// Called from startup before frontend runs; frontend will later check Pro and may call SetAutoRunEnabled(false).
func (a *App) syncAutoRunWithConfig() {
	cfg, err := a.storage.Load()
	if err != nil {
		return
	}
	enabled, _ := autostart.IsEnabled()
	if cfg.AutoRun && !enabled {
		_ = autostart.Enable()
	} else if !cfg.AutoRun && enabled {
		_ = autostart.Disable()
	}
}

// GetAutoRunEnabled returns whether the app is currently set to launch at login (system state).
func (a *App) GetAutoRunEnabled() (bool, error) {
	if err := a.ensureReady(); err != nil {
		return false, err
	}
	return autostart.IsEnabled()
}

// SetAutoRunEnabled enables or disables launch at login and persists to config.
func (a *App) SetAutoRunEnabled(enabled bool) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	_, err := a.storage.Update(func(cfg *conf.Config) error {
		cfg.AutoRun = enabled
		return nil
	})
	if err != nil {
		return err
	}
	if enabled {
		return autostart.Enable()
	}
	return autostart.Disable()
}

func (a *App) GetTrafficMonitorEnabled() (bool, error) {
	if err := a.ensureReady(); err != nil {
		return true, err
	}
	cfg, err := a.storage.Load()
	if err != nil {
		return true, err
	}
	return cfg.TrafficMonitorEnabled, nil
}

func (a *App) SetTrafficMonitorEnabled(enabled bool) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	_, err := a.storage.Update(func(cfg *conf.Config) error {
		cfg.TrafficMonitorEnabled = enabled
		return nil
	})
	return err
}

// GetConfigPath returns the absolute path of the current config file.
func (a *App) GetConfigPath() (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(a.storage.Path())
	if err != nil {
		return a.storage.Path(), nil
	}
	return abs, nil
}

// ExportConfig copies the current config.toml to destPath.
// ExportConfigWithDialog opens a save-file dialog, then copies the config.
// Returns empty string if the user cancelled.
func (a *App) ExportConfigWithDialog() error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	destPath, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		DefaultFilename: "config.toml",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "TOML Config (*.toml)", Pattern: "*.toml"},
		},
	})
	if err != nil {
		return fmt.Errorf("file dialog: %w", err)
	}
	if strings.TrimSpace(destPath) == "" {
		return nil // user cancelled
	}
	return a.ExportConfig(destPath)
}

func (a *App) ExportConfig(destPath string) error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		return fmt.Errorf("destination path is empty")
	}

	cfg, err := a.storage.Load()
	if err != nil {
		return err
	}
	if filepath.Clean(destPath) == filepath.Clean(a.storage.Path()) {
		return fmt.Errorf("cannot export over the active config")
	}
	if err = os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	if err = os.WriteFile(destPath, conf.ExportTOML(cfg), 0600); err != nil {
		return err
	}

	slog.Info("config exported", "dest", destPath)
	return nil
}

// SelectImportFile opens a file picker and returns the selected path. Returns empty string if cancelled.
func (a *App) SelectImportFile() (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	srcPath, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "TOML Config (*.toml)", Pattern: "*.toml"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("file dialog: %w", err)
	}
	return strings.TrimSpace(srcPath), nil
}

// ImportConfig replaces the current config with the TOML file at srcPath.
// It validates the file first, then stops all running tunnels, replaces the
// config file, and restarts auto-start tunnels.
func (a *App) ImportConfig(srcPath string) error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	srcPath = strings.TrimSpace(srcPath)
	if srcPath == "" {
		return fmt.Errorf("source path is empty")
	}

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	imported, err := conf.ParseConfigTOML(data)
	if err != nil {
		return fmt.Errorf("invalid config file: %w", err)
	}
	running := a.tunnel.RunningIDs()
	a.tunnel.Shutdown()
	_, err = a.storage.Update(func(c *conf.Config) error {
		baseline, baselines := c.SyncBaseline, c.SyncBaselines
		*c = *imported
		c.SyncBaseline, c.SyncBaselines = baseline, baselines
		for i := range c.Tunnels {
			c.Tunnels[i].Status = "stopped"
			c.Tunnels[i].LastError = ""
		}
		return nil
	})
	if err != nil {
		for _, id := range running {
			_, _ = a.tunnel.Toggle(id, a.tunnelStartLimit)
		}
		return err
	}
	_ = a.tunnel.StartAutoStart(a.tunnelStartLimit())

	slog.Info("config imported", "src", srcPath)
	return nil
}

// OpenConfigDir opens the config file's parent directory in the OS file manager.
// It supports macOS, Windows and Linux (via xdg-open).
func (a *App) OpenConfigDir() error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	dir := filepath.Dir(a.storage.Path())

	return openLocalDirectory(dir)
}

// OpenReportEmail opens the default mail client with a prefilled report draft.
// Success means the OS accepted the open request; it does not guarantee email delivery.
func (a *App) OpenReportEmail(payload OpenReportEmailPayload) OpenReportEmailResult {
	subject := trimForMailto(payload.Subject, maxMailtoSubjectLen)
	body := trimForMailto(payload.Body, maxMailtoBodyLen)
	mailtoURL := "mailto:" + aiReportSupportEmail + "?subject=" + encodeMailtoQueryValue(subject) + "&body=" + encodeMailtoQueryValue(body)

	name, args := reportEmailCommand(runtime.GOOS, mailtoURL)
	if strings.TrimSpace(name) == "" {
		return OpenReportEmailResult{Success: false, Error: "unsupported platform"}
	}

	cmd := exec.Command(name, args...)
	if err := cmd.Run(); err != nil {
		return OpenReportEmailResult{Success: false, Error: err.Error()}
	}
	return OpenReportEmailResult{Success: true}
}

func encodeMailtoQueryValue(value string) string {
	encoded := url.QueryEscape(value)
	// Use %20 for spaces to avoid clients showing "+" literally in draft fields.
	return strings.ReplaceAll(encoded, "+", "%20")
}

func reportEmailCommand(goos, mailtoURL string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{mailtoURL}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", mailtoURL}
	case "linux":
		return "xdg-open", []string{mailtoURL}
	default:
		return "", nil
	}
}

func trimForMailto(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}
