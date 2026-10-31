package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"loris-tunnel/internal/cloudsync"
	"loris-tunnel/internal/conf"
)

func (a *App) initializeCloudSync() error {
	environment := a.license.Environment()
	server := a.cloudSyncServer()
	directory := filepath.Dir(a.storage.Path())
	path := filepath.Join(directory, cloudsync.SettingsFileForEnvironment(environment))
	local, err := a.storage.Load()
	if err != nil {
		return err
	}
	baseline := local.SyncBaselines[environment]
	if baseline.SpaceID == "" && local.SyncBaseline.ServerURL == server {
		baseline = local.SyncBaseline
	}
	if err = cloudsync.MigrateLegacy(path, filepath.Join(directory, cloudsync.SettingsFileName), a.machineID, server, baseline); err != nil {
		return err
	}
	if home, err := os.UserConfigDir(); err == nil {
		legacy := filepath.Join(home, "loris-tunnel", cloudsync.SettingsFileName)
		if err = cloudsync.MigrateLegacy(path, legacy, a.machineID, server, baseline); err != nil {
			return err
		}
	}
	// Persist both environments before removing any old TOML metadata.
	if local.SyncBaseline.SpaceID != "" {
		env := "prod"
		if local.SyncBaseline.ServerURL == "http://127.0.0.1:8000" {
			env = "dev"
		}
		if local.SyncBaselines[env].SpaceID == "" {
			local.SetSyncBaseline(env, local.SyncBaseline)
		}
	}
	for env, b := range local.SyncBaselines {
		if err = cloudsync.MigrateBaseline(filepath.Join(directory, cloudsync.SettingsFileForEnvironment(env)), b); err != nil {
			return err
		}
	}
	if local.SyncBaseline.SpaceID != "" || len(local.SyncBaselines) > 0 {
		if _, err = a.storage.Update(func(c *conf.Config) error { c.SyncBaseline = conf.SyncBaseline{}; c.SyncBaselines = nil; return nil }); err != nil {
			return err
		}
	}
	hostname, _ := os.Hostname()
	name := hostname + " · " + runtime.GOOS
	service, err := cloudsync.New(
		path,
		a.machineID,
		name,
		cloudsync.SystemVault{},
		cloudsync.Hooks{
			Snapshot: func() ([]byte, error) {
				a.configGate.Lock()
				defer a.configGate.Unlock()
				if a.cloudSync != nil && a.cloudSync.Stopped() {
					return nil, fmt.Errorf("cloud sync stopped; restart the app")
				}
				return a.storage.ReadRaw()
			},
			Apply: a.applyCloudConfig,
			Changed: func(st cloudsync.Status) {
				if a.ctx != nil {
					wailsruntime.EventsEmit(a.ctx, "cloud-sync:status", st)
				}
			},
		},
	)
	if err != nil {
		return err
	}
	if err = service.ConfigureEnvironment(a.cloudSyncServer(), name); err != nil {
		service.Close()
		return err
	}
	a.cloudSync = service
	a.storage.ObserveSave(func(c *conf.Config) {
		service.Notify()
	})
	return nil
}
func (a *App) cloudSyncServer() string {
	return strings.TrimSuffix(a.license.BaseURL(), "/api/v1")
}
func (a *App) GenerateCloudSyncToken() (string, error) {
	if e := a.syncReady(); e != nil {
		return "", e
	}
	return a.cloudSync.GenerateToken(a.cloudSyncServer(), a.cloudSync.Status().DeviceName)
}
func (a *App) syncReady() error {
	if e := a.ensureReady(); e != nil {
		return e
	}
	if a.cloudSync == nil {
		if a.cloudSyncErr != nil {
			return a.cloudSyncErr
		}
		return fmt.Errorf("cloud sync is not initialized")
	}
	if a.cloudSync.Stopped() {
		return fmt.Errorf("cloud sync stopped; restart the app")
	}
	return nil
}
func (a *App) GetCloudSyncStatus() (cloudsync.Status, error) {
	if e := a.syncReady(); e != nil {
		return cloudsync.Status{}, e
	}
	status := a.cloudSync.Status()
	status.ServerURL = a.cloudSyncServer()
	return status, nil
}
func (a *App) SetCloudSyncEnabled(enabled bool) (cloudsync.Status, error) {
	if e := a.syncReady(); e != nil {
		return cloudsync.Status{}, e
	}
	return a.cloudSync.SetEnabled(enabled)
}
func (a *App) TestCloudConnection(token string) (cloudsync.Remote, error) {
	if e := a.syncReady(); e != nil {
		return cloudsync.Remote{}, e
	}
	return a.cloudSync.Test(a.cloudSyncServer(), token)
}
func (a *App) ConnectCloudSync(token string) (cloudsync.Status, error) {
	if e := a.syncReady(); e != nil {
		return cloudsync.Status{}, e
	}
	return a.cloudSync.Connect(a.cloudSyncServer(), token, a.cloudSync.Status().DeviceName)
}
func (a *App) DisconnectCloudSync() error {
	if e := a.syncReady(); e != nil {
		return e
	}
	return a.cloudSync.Disconnect()
}
func (a *App) SetCloudSyncPreferences(automatic bool, deviceName string) error {
	if e := a.syncReady(); e != nil {
		return e
	}
	return a.cloudSync.SetPreferences(automatic, a.cloudSync.Status().DeviceName)
}
func (a *App) CloudSyncNow() (cloudsync.Status, error) {
	if e := a.syncReady(); e != nil {
		return cloudsync.Status{}, e
	}
	return a.cloudSync.Sync()
}
func (a *App) CheckCloudSyncOnFocus() {
	if a.cloudSync != nil {
		a.cloudSync.Focus()
	}
}
func (a *App) ResolveCloudSync(choice string, revision int64, fingerprint string) (cloudsync.Status, error) {
	if e := a.syncReady(); e != nil {
		return cloudsync.Status{}, e
	}
	return a.cloudSync.Resolve(choice, revision, fingerprint)
}
func (a *App) CopyCloudSyncToken() error {
	if e := a.syncReady(); e != nil {
		return e
	}
	token, e := a.cloudSync.Token()
	if e != nil {
		return e
	}
	if a.ctx == nil {
		return fmt.Errorf("clipboard is unavailable")
	}
	return wailsruntime.ClipboardSetText(a.ctx, token)
}

func (a *App) applyCloudConfig(data []byte, expected string) error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if a.cloudSync != nil && a.cloudSync.Stopped() {
		return fmt.Errorf("cloud sync stopped; restart the app")
	}
	if a.cloudSync != nil && !a.cloudSync.Status().Enabled {
		return fmt.Errorf("cloud sync is disabled")
	}
	if _, e := conf.ParseConfigTOML(data); e != nil {
		return e
	}
	before, e := a.storage.ReadRaw()
	if e != nil {
		return e
	}
	if cloudsync.Fingerprint(before) != expected {
		return fmt.Errorf("local configuration changed; confirm again")
	}
	running := a.tunnel.RunningIDs()
	a.tunnel.Shutdown()
	if e = a.storage.RestoreRaw(data, expected); e != nil {
		_ = a.tunnel.StartPreviouslyRunning(running, a.tunnelStartLimit())
		return e
	}
	// Autostart failures are connection errors, not a failed file restoration.
	if e = a.tunnel.StartAutoStart(a.tunnelStartLimit()); e != nil {
		return &cloudsync.AppliedError{Err: fmt.Errorf("file restored; auto-start failed: %w", e)}
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "cloud-sync:applied")
	}
	return nil
}
