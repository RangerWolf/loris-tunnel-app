package biz

import (
	"context"
	"fmt"

	"loris-tunnel/internal/climaint"
	"loris-tunnel/internal/model"
)

func (b *TunnelBiz) cliManager(provider string) (*climaint.Manager, error) {
	b.cliMu.Lock()
	defer b.cliMu.Unlock()
	if b.cliManagers == nil {
		b.cliManagers = map[string]*climaint.Manager{}
	}
	if manager := b.cliManagers[provider]; manager != nil {
		return manager, nil
	}
	var spec climaint.Spec
	switch provider {
	case model.ProviderDevTunnel:
		spec = b.dev.MaintenanceSpec()
	case model.ProviderCloudflare:
		spec = b.cloudflare.MaintenanceSpec()
	case model.ProviderFRP:
		spec = b.frp.MaintenanceSpec()
	default:
		return nil, fmt.Errorf("Unsupported CLI provider")
	}
	manager := climaint.New(spec)
	b.cliManagers[provider] = manager
	return manager, nil
}

// Caller holds b.mu. The same lock reserves starts and binary replacement.
func (b *TunnelBiz) cliBlockedLocked(provider string) (string, int) {
	if b.cliChanging[provider] {
		return "busy", 0
	}
	count := 0
	for _, p := range b.runProvider {
		if p == provider {
			count++
		}
	}
	for _, task := range b.starting {
		if task != nil && task.ProviderName() == provider {
			count++
		}
	}
	if count > 0 {
		return "running", count
	}
	if provider == model.ProviderDevTunnel && b.dev.MaintenanceBusy() || provider == model.ProviderCloudflare && b.cloudflare.MaintenanceBusy() {
		return "login", 0
	}
	return "", 0
}
func (b *TunnelBiz) beginCLIChange(provider string) (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	reason, _ := b.cliBlockedLocked(provider)
	if reason != "" {
		return nil, fmt.Errorf("CLI replacement blocked: %s; stop its tunnels or finish login first", reason)
	}
	if b.cliChanging == nil {
		b.cliChanging = map[string]bool{}
	}
	b.cliChanging[provider] = true
	return func() { b.mu.Lock(); delete(b.cliChanging, provider); b.mu.Unlock() }, nil
}
func (b *TunnelBiz) GetCLIMaintenance(ctx context.Context, provider string) (climaint.Status, error) {
	manager, err := b.cliManager(provider)
	if err != nil {
		return climaint.Status{}, err
	}
	status := manager.Status(ctx)
	b.mu.Lock()
	status.BlockedReason, status.BlockedCount = b.cliBlockedLocked(provider)
	b.mu.Unlock()
	return status, nil
}
func (b *TunnelBiz) CheckCLIUpdate(ctx context.Context, provider string, force bool) (climaint.Status, error) {
	manager, err := b.cliManager(provider)
	if err != nil {
		return climaint.Status{}, err
	}
	return manager.Check(ctx, force)
}
func (b *TunnelBiz) ImportCLIUpdate(ctx context.Context, provider, path string) (climaint.Status, error) {
	manager, err := b.cliManager(provider)
	if err != nil {
		return climaint.Status{}, err
	}
	// Imports are staged only; the user applies the displayed version separately.
	b.mu.Lock()
	reason, _ := b.cliBlockedLocked(provider)
	b.mu.Unlock()
	if reason != "" {
		return climaint.Status{}, fmt.Errorf("CLI import blocked: %s", reason)
	}
	return manager.Import(ctx, path)
}
func (b *TunnelBiz) ApplyCLIUpdate(ctx context.Context, provider, id string) (climaint.Status, error) {
	manager, err := b.cliManager(provider)
	if err != nil {
		return climaint.Status{}, err
	}
	return manager.Apply(ctx, id, func() (func(), error) { return b.beginCLIChange(provider) })
}
func (b *TunnelBiz) RestorePreviousCLI(ctx context.Context, provider string) (climaint.Status, error) {
	manager, err := b.cliManager(provider)
	if err != nil {
		return climaint.Status{}, err
	}
	return manager.Restore(ctx, func() (func(), error) { return b.beginCLIChange(provider) })
}
func (b *TunnelBiz) CancelCLIUpdate(provider string) bool {
	if manager, err := b.cliManager(provider); err == nil {
		return manager.Cancel()
	}
	return false
}
func (b *TunnelBiz) installManagedCLI(ctx context.Context, provider string) error {
	status, err := b.CheckCLIUpdate(ctx, provider, true)
	if err != nil {
		return err
	}
	_, err = b.ApplyCLIUpdate(ctx, provider, status.CandidateID)
	return err
}
