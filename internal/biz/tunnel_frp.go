package biz

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"loris-tunnel/internal/frp"
	"loris-tunnel/internal/model"
)

var frpProxyName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func validateFRPPayload(p model.TunnelPayload) error {
	if p.FRP == nil || p.FRP.ServerID == "" {
		return fmt.Errorf("select an FRP server")
	}
	if !frpProxyName.MatchString(p.FRP.ProxyName) {
		return fmt.Errorf("FRP proxy name must be 1–64 letters, digits, underscores or hyphens")
	}
	return frp.ValidateProxy(p.Mode, p.LocalHost, p.LocalPort, p.FRP.RemotePort, p.FRP.Domain)
}
func (b *TunnelBiz) validateFRPBinding(items []model.Tunnel, self int, p model.TunnelPayload) error {
	if p.Provider != model.ProviderFRP {
		return nil
	}
	server, err := b.frp.Store.Server(p.FRP.ServerID)
	if err != nil {
		return err
	}
	for _, t := range items {
		if t.ID == self || t.ProviderName() != model.ProviderFRP || t.FRP == nil {
			continue
		}
		other, err := b.frp.Store.Server(t.FRP.ServerID)
		if err != nil || other.Host != server.Host || other.Port != server.Port {
			continue
		}
		if other.User == server.User && t.FRP.ProxyName == p.FRP.ProxyName || t.Mode == p.Mode && (p.Mode == "tcp" && t.FRP.RemotePort == p.FRP.RemotePort || p.Mode == "http" && t.FRP.Domain == p.FRP.Domain) {
			return fmt.Errorf("FRP proxy name or public endpoint is already used by %s", t.Name)
		}
	}
	return nil
}
func (b *TunnelBiz) FRPStatus(ctx context.Context) (frp.Status, error) { return b.frp.Inspect(ctx) }
func (b *TunnelBiz) MatchFRPImport(server frp.Server) ([]string, error) {
	return b.frp.Store.MatchImport(server)
}

func (b *TunnelBiz) SaveFRPServer(server frp.Server) (frp.Server, error) {
	b.frpMu.Lock()
	defer b.frpMu.Unlock()
	cfg, err := b.storage.Load()
	if err != nil {
		return frp.Server{}, err
	}
	for _, t := range cfg.Tunnels {
		if t.FRP != nil && t.FRP.ServerID == server.ID && b.isRunning(t.ID) {
			return frp.Server{}, fmt.Errorf("stop linked FRP tunnels before editing this server")
		}
	}
	return b.frp.Store.SaveServer(server)
}
func (b *TunnelBiz) DeleteFRPServer(id string) error {
	b.frpMu.Lock()
	defer b.frpMu.Unlock()
	cfg, err := b.storage.Load()
	if err != nil {
		return err
	}
	for _, t := range cfg.Tunnels {
		if t.FRP != nil && t.FRP.ServerID == id {
			return fmt.Errorf("remove linked tunnel %s before deleting this server", t.Name)
		}
	}
	return b.frp.Store.DeleteServer(id)
}
func (b *TunnelBiz) InstallFRP(ctx context.Context) error {
	return b.installManagedCLI(ctx, model.ProviderFRP)
}
func (b *TunnelBiz) SelectFRPBinary(ctx context.Context, path string) error {
	status, err := b.ImportCLIUpdate(ctx, model.ProviderFRP, path)
	if err != nil {
		return err
	}
	_, err = b.ApplyCLIUpdate(ctx, model.ProviderFRP, status.CandidateID)
	return err
}

func (b *TunnelBiz) frpActive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.runProvider {
		if p == model.ProviderFRP {
			return true
		}
	}
	for _, t := range b.starting {
		if t.ProviderName() == model.ProviderFRP {
			return true
		}
	}
	return false
}
func (b *TunnelBiz) newStartedFRPRuntime(task *model.Tunnel) (tunnelRuntime, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b.mu.Lock()
	if b.starting[task.ID] != task {
		b.mu.Unlock()
		return nil, context.Canceled
	}
	b.devStartCancel[task] = cancel
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.devStartCancel, task); b.mu.Unlock() }()
	if task.FRP == nil {
		return nil, fmt.Errorf("FRP configuration required")
	}
	// Serialize start setup with local server/client changes.
	b.frpMu.Lock()
	server, err := b.frp.Store.Server(task.FRP.ServerID)
	b.frpMu.Unlock()
	if err != nil {
		return nil, err
	}
	if task.Status == "error" {
		return b.frp.Retry(ctx, server, task.FRP.ProxyName, task.Mode, task.LocalHost, task.LocalPort, task.FRP.RemotePort, task.FRP.Domain)
	}
	return b.frp.Start(ctx, server, task.FRP.ProxyName, task.Mode, task.LocalHost, task.LocalPort, task.FRP.RemotePort, task.FRP.Domain)
}
