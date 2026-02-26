package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"loris-tunnel/internal/biz"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/device"
	"loris-tunnel/internal/model"
)

// App struct
type App struct {
	ctx       context.Context
	storage   *conf.Storage
	jumper    *biz.JumperBiz
	tunnel    *biz.TunnelBiz
	machineID string
	initErr   error
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

	return &App{
		storage:   storage,
		jumper:    biz.NewJumperBiz(storage),
		tunnel:    biz.NewTunnelBiz(storage),
		machineID: device.MachineID(),
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
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	slog.Info("app startup")
	if err := a.ensureReady(); err == nil {
		_ = a.tunnel.StartAutoStart()
	}
}

func (a *App) shutdown(ctx context.Context) {
	_ = ctx
	slog.Info("app shutdown")
	if a.tunnel != nil {
		a.tunnel.Shutdown()
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

func (a *App) GetMachineID() (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.machineID) == "" {
		a.machineID = device.MachineID()
	}
	return a.machineID, nil
}
