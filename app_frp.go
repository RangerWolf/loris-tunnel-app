package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"loris-tunnel/internal/frp"
)

func (a *App) ParseFRPCImport(content, format string) (*frp.ImportResult, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	result, err := frp.ParseImport(content, format)
	if err != nil {
		return nil, err
	}
	result.MatchedServerIDs, err = a.tunnel.MatchFRPImport(result.Server)
	// Tokens remain in the pasted source, never in preview objects.
	result.Server.HasToken = result.Server.Token != ""
	result.Server.Token = ""
	return result, err
}

// ResolveFRPCImportServer re-parses the source so preview data cannot replace credentials.
func (a *App) ResolveFRPCImportServer(content, format, existingID, name string) (frp.Server, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return frp.Server{}, err
	}
	result, err := frp.ParseImport(content, format)
	if err != nil {
		return frp.Server{}, err
	}
	if len(result.Errors) > 0 {
		return frp.Server{}, fmt.Errorf("FRPC configuration has unsupported or invalid client options")
	}
	valid := false
	for _, proxy := range result.Proxies {
		if len(proxy.Errors) == 0 {
			valid = true
		}
	}
	if !valid {
		return frp.Server{}, fmt.Errorf("FRPC configuration has no supported proxies")
	}
	matched, err := a.tunnel.MatchFRPImport(result.Server)
	if err != nil {
		return frp.Server{}, err
	}
	if existingID != "" {
		if !slices.Contains(matched, existingID) {
			return frp.Server{}, fmt.Errorf("FRP server address or port changed; parse the configuration again")
		}
		return frp.Server{ID: existingID}, nil
	}
	result.Server.Name = strings.TrimSpace(name)
	return a.tunnel.SaveFRPServer(result.Server)
}

func (a *App) GetFRPStatus() (frp.Status, error) {
	if err := a.ensureReady(); err != nil {
		return frp.Status{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	return a.tunnel.FRPStatus(ctx)
}
func (a *App) SaveFRPServer(server frp.Server) (frp.Server, error) {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return frp.Server{}, err
	}
	return a.tunnel.SaveFRPServer(server)
}
func (a *App) DeleteFRPServer(id string) error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	return a.tunnel.DeleteFRPServer(id)
}
func (a *App) InstallFRPClient() error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return a.tunnel.InstallFRP(ctx)
}
func (a *App) SelectFRPClient() error {
	a.configGate.Lock()
	defer a.configGate.Unlock()
	if err := a.ensureReady(); err != nil {
		return err
	}
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "Select frpc executable"})
	if err != nil || strings.TrimSpace(path) == "" {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	return a.tunnel.SelectFRPBinary(ctx, path)
}
