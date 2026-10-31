package biz

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/frp"
	"loris-tunnel/internal/model"
)

func TestFRPCRUDValidationAndLocalCredentials(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	b := NewTunnelBiz(storage)
	server, err := b.SaveFRPServer(frp.Server{Name: "local", Host: "127.0.0.1", Token: "must-not-sync", TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	payload := model.TunnelPayload{Name: "web", Provider: model.ProviderFRP, Mode: "tcp", LocalHost: "127.0.0.1", LocalPort: 3000, FRP: &model.FRPTunnelConfig{ServerID: server.ID, ProxyName: "web", RemotePort: 13000}, AutoStart: true}
	tunnel, err := b.Create(payload)
	if err != nil {
		t.Fatal(err)
	}
	if tunnel.ProviderName() != model.ProviderFRP || tunnel.FRP == nil || len(tunnel.JumperIDs) != 0 {
		t.Fatal("FRP normalized to another provider")
	}
	duplicate := payload
	duplicate.FRP = &model.FRPTunnelConfig{ServerID: server.ID, ProxyName: "different", RemotePort: 13000}
	if _, err = b.Create(duplicate); err == nil {
		t.Fatal("duplicate remote endpoint accepted")
	}
	if err = b.DeleteFRPServer(server.ID); err == nil {
		t.Fatal("linked server removed")
	}
	cfg, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	clone := cfg.Clone()
	clone.Tunnels[0].FRP.ProxyName = "changed"
	if cfg.Tunnels[0].FRP.ProxyName != "web" {
		t.Fatal("FRP config shared across clone")
	}
	raw, err := os.ReadFile(storage.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-not-sync") {
		t.Fatal("credential leaked into normal config")
	}
	items, err := b.List()
	if err != nil || items[0].PublicURL != "127.0.0.1:13000" {
		t.Fatalf("FRP address missing: %v", err)
	}
	payload.FRP = &model.FRPTunnelConfig{ServerID: server.ID, ProxyName: "web", Domain: "dev.example.com"}
	payload.Mode = "http"
	if _, err = b.Update(tunnel.ID, payload); err != nil {
		t.Fatal(err)
	}
	if err = b.Delete(tunnel.ID); err != nil {
		t.Fatal(err)
	}
	if err = b.DeleteFRPServer(server.ID); err != nil {
		t.Fatal(err)
	}
}
