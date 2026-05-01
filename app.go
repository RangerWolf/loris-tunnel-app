package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/device"
	"loris-tunnel/internal/license"
	"loris-tunnel/internal/model"
	"loris-tunnel/internal/sshconfig"
	"loris-tunnel/internal/traytext"
	"loris-tunnel/internal/uilocale"
	"loris-tunnel/internal/updater"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const usageHeartbeatInterval = 2 * time.Hour

// App struct
type App struct {
	ctx       context.Context
	storage   *conf.Storage
	jumper    *biz.JumperBiz
	tunnel    *biz.TunnelBiz
	updater   *updater.Service
	license   *license.Client
	aiDebug   *aidebug.Service
	machineID string
	initErr   error

	trayMu   sync.Mutex
	trayShow *systray.MenuItem
	trayQuit *systray.MenuItem

	allowClose atomic.Bool

	usageReporterStop chan struct{}
	usageReporterWG   sync.WaitGroup
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
		tunnel:    biz.NewTunnelBiz(storage),
		updater:   updater.NewDefaultService(),
		license:   licenseClient,
		aiDebug:   aidebug.NewService(licenseClient.BaseURL(), machineID),
		machineID: machineID,
	}
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
		go func() {
			if err := a.tunnel.StartAutoStart(); err != nil {
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
	slog.Info("app shutdown")
	a.stopUsageReporter()
	if a.tunnel != nil {
		a.tunnel.Shutdown()
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

func (a *App) reportUsageEvent(eventType string) {
	if a.license == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := a.license.ReportUsageEvent(ctx, model.UsageEventRequest{
		MachineID: a.machineID,
		EventType: eventType,
		Platform:  runtime.GOOS,
		ClientTS:  time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		slog.Warn("report usage event failed", "event_type", eventType, "error", err)
	}
}

func (a *App) ensureReady() error {
	if a.initErr != nil {
		return a.initErr
	}
	if a.storage == nil || a.jumper == nil || a.tunnel == nil {
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
	jumpers, err := a.jumper.List()
	if err != nil {
		return model.State{}, err
	}
	tunnels, err := a.tunnel.List()
	if err != nil {
		return model.State{}, err
	}
	return model.State{
		Jumpers: append([]model.Jumper{}, jumpers...),
		Tunnels: append([]model.Tunnel{}, tunnels...),
	}, nil
}

func (a *App) ListJumpers() ([]model.Jumper, error) {
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
	if err := a.ensureReady(); err != nil {
		return model.Jumper{}, err
	}
	return a.jumper.Create(payload)
}

func (a *App) UpdateJumper(id int, payload model.JumperPayload) (model.Jumper, error) {
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
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.jumper.Delete(id)
}

func (a *App) ListTunnels() ([]model.Tunnel, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.tunnel.List()
}

func (a *App) CreateTunnel(payload model.TunnelPayload) (model.Tunnel, error) {
	if err := a.ensureReady(); err != nil {
		return model.Tunnel{}, err
	}
	return a.tunnel.Create(payload)
}

func (a *App) UpdateTunnel(id int, payload model.TunnelPayload) (model.Tunnel, error) {
	if err := a.ensureReady(); err != nil {
		return model.Tunnel{}, err
	}
	return a.tunnel.Update(id, payload)
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
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.tunnel.Delete(id)
}

func (a *App) ToggleTunnel(id int) (model.Tunnel, error) {
	if err := a.ensureReady(); err != nil {
		return model.Tunnel{}, err
	}
	return a.tunnel.Toggle(id)
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
	if err := a.ensureReady(); err != nil {
		return err
	}
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		return fmt.Errorf("destination path is empty")
	}

	src, err := os.Open(a.storage.Path())
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy config file: %w", err)
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
	if err := a.ensureReady(); err != nil {
		return err
	}
	srcPath = strings.TrimSpace(srcPath)
	if srcPath == "" {
		return fmt.Errorf("source path is empty")
	}

	// Validate: can we parse it as a valid config?
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	if _, err := conf.ParseConfigTOML(data); err != nil {
		return fmt.Errorf("invalid config file: %w", err)
	}

	// Stop all running tunnels.
	if a.tunnel != nil {
		a.tunnel.Shutdown()
	}

	// Overwrite config file atomically.
	tmpPath := a.storage.Path() + ".import.tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmpPath, a.storage.Path()); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}

	// Reinitialise biz layer so the new config takes effect.
	a.jumper = biz.NewJumperBiz(a.storage)
	a.tunnel = biz.NewTunnelBiz(a.storage)

	// Restart auto-start tunnels.
	_ = a.tunnel.StartAutoStart()

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

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		// Most desktop Linux environments provide xdg-open.
		cmd = exec.Command("xdg-open", dir)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open config dir: %w", err)
	}
	return nil
}
