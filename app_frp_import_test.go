package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loris-tunnel/internal/biz"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/model"
)

func TestFRPCImportPreviewAndSave(t *testing.T) {
	storage, err := conf.NewStorage(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{storage: storage, jumper: biz.NewJumperBiz(storage), group: biz.NewGroupBiz(storage), tunnel: biz.NewTunnelBiz(storage)}
	const source = `{"serverAddr":"example.com","auth":{"token":"private-import-token"},"proxies":[{"name":"original-proxy-name-longer-than-display","localPort":22,"remotePort":6000}]}`
	preview, err := a.ParseFRPCImport(source, "json")
	if err != nil || len(preview.Errors) != 0 {
		t.Fatal("preview failed", err)
	}
	data, _ := json.Marshal(preview)
	if strings.Contains(string(data), "private-import-token") || !preview.Server.HasToken {
		t.Fatal("preview exposed or lost credentials")
	}
	server, err := a.ResolveFRPCImportServer(source, "json", "", "Imported")
	if err != nil || server.ID == "" || server.Token != "" {
		t.Fatal("save failed", err)
	}
	second, err := a.ResolveFRPCImportServer(source, "json", "", "Second")
	if err != nil {
		t.Fatal(err)
	}
	preview, err = a.ParseFRPCImport(source, "json")
	if err != nil || len(preview.MatchedServerIDs) != 2 {
		t.Fatal("matching candidates missing", err)
	}
	if _, err := a.ResolveFRPCImportServer(source, "json", second.ID, ""); err != nil {
		t.Fatal("cannot select second match", err)
	}
	changed := strings.ReplaceAll(source, "private-import-token", "different-token")
	if _, err := a.ResolveFRPCImportServer(changed, "json", server.ID, ""); err != nil {
		t.Fatal("cannot reuse server with different import token", err)
	}
	changed = strings.ReplaceAll(source, "example.com", "other.example.com")
	if _, err := a.ResolveFRPCImportServer(changed, "json", server.ID, ""); err == nil {
		t.Fatal("reused server with different endpoint")
	}
	proxy := preview.Proxies[0]
	tunnel, err := a.CreateTunnel(model.TunnelPayload{Name: "Display name", Provider: model.ProviderFRP, Mode: proxy.Type, LocalHost: proxy.LocalIP, LocalPort: proxy.LocalPort, Status: "stopped", FRP: &model.FRPTunnelConfig{ServerID: server.ID, ProxyName: proxy.Name, RemotePort: proxy.RemotePort}})
	if err != nil || tunnel.AutoStart || tunnel.Status != "stopped" || tunnel.FRP.ProxyName != proxy.Name {
		t.Fatal("imported tunnel cannot be saved without frpc", err)
	}
	data, err = os.ReadFile(storage.Path())
	if err != nil || strings.Contains(string(data), "private-import-token") {
		t.Fatal("credentials in normal configuration", err)
	}
}
