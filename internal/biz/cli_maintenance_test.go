package biz

import (
	"testing"

	"loris-tunnel/internal/model"
)

func TestCLIMaintenanceGuardsStartsAndLogin(t *testing.T) {
	b := NewTunnelBiz(newCloudflareStorage(t))
	release, err := b.beginCLIChange(model.ProviderCloudflare)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.beginCLIChange(model.ProviderCloudflare); err == nil {
		t.Fatal("concurrent replacement allowed")
	}
	if _, err := b.beginAsyncStart(model.Tunnel{ID: 1, Provider: model.ProviderCloudflare}, nil, 0); err == nil {
		t.Fatal("start allowed during replacement")
	}
	if err := b.StartCloudflareZoneAuthorization("", nil, nil); err == nil {
		t.Fatal("login allowed during replacement")
	}
	// Another provider remains available.
	other, err := b.beginCLIChange(model.ProviderFRP)
	if err != nil {
		t.Fatal(err)
	}
	other()
	release()
	for _, provider := range []string{model.ProviderCloudflare, model.ProviderDevTunnel, model.ProviderFRP} {
		b.mu.Lock()
		b.runProvider[1] = provider
		b.mu.Unlock()
		if _, err := b.beginCLIChange(provider); err == nil {
			t.Fatalf("replacement allowed with running %s", provider)
		}
		b.mu.Lock()
		delete(b.runProvider, 1)
		b.starting[1] = &model.Tunnel{ID: 1, Provider: provider}
		b.mu.Unlock()
		if _, err := b.beginCLIChange(provider); err == nil {
			t.Fatalf("replacement allowed with starting %s", provider)
		}
		b.mu.Lock()
		delete(b.starting, 1)
		b.mu.Unlock()
	}
}
func TestAutomaticStartSkipsProviderBeingMaintained(t *testing.T) {
	b := NewTunnelBiz(newCloudflareStorage(t))
	created, err := b.Create(model.TunnelPayload{Name: "auto", Provider: model.ProviderCloudflare, Mode: "http", LocalHost: "127.0.0.1", LocalPort: 3000, AutoStart: true, CloudflareIDMode: "bind", Cloudflare: &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, Hostname: "dev.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	release, err := b.beginCLIChange(model.ProviderCloudflare)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := b.StartAutoStart(0); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	_, started := b.starting[created.ID]
	b.mu.Unlock()
	if started {
		t.Fatal("automatic start bypassed maintenance guard")
	}
}
