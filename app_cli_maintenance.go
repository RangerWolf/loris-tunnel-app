package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"loris-tunnel/internal/climaint"
)

func (a *App) GetCLIMaintenance(provider string) (climaint.Status, error) {
	if err := a.ensureReady(); err != nil {
		return climaint.Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.tunnel.GetCLIMaintenance(ctx, provider)
}
func (a *App) CheckCLIUpdate(provider string, force bool) (climaint.Status, error) {
	if err := a.ensureReady(); err != nil {
		return climaint.Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	return a.tunnel.CheckCLIUpdate(ctx, provider, force)
}
func (a *App) ImportCLIUpdate(provider string) (climaint.Status, error) {
	if err := a.ensureReady(); err != nil {
		return climaint.Status{}, err
	}
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "Select CLI executable"})
	if err != nil {
		return climaint.Status{}, err
	}
	if strings.TrimSpace(path) == "" {
		return a.GetCLIMaintenance(provider)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.tunnel.ImportCLIUpdate(ctx, provider, path)
}
func (a *App) ApplyCLIUpdate(provider, id string) (climaint.Status, error) {
	if err := a.ensureReady(); err != nil {
		return climaint.Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	return a.tunnel.ApplyCLIUpdate(ctx, provider, id)
}
func (a *App) RestorePreviousCLI(provider string) (climaint.Status, error) {
	if err := a.ensureReady(); err != nil {
		return climaint.Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.tunnel.RestorePreviousCLI(ctx, provider)
}
func (a *App) CancelCLIUpdate(provider string) bool {
	if a.tunnel != nil {
		return a.tunnel.CancelCLIUpdate(provider)
	}
	return true
}
func (a *App) OpenCLIDirectory(provider string) error {
	status, err := a.GetCLIMaintenance(provider)
	if err != nil {
		return err
	}
	if status.Path == "" {
		return nil
	}
	return openLocalDirectory(filepath.Dir(status.Path))
}
func openLocalDirectory(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
