package biz

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loris-tunnel/internal/cloudflared"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/model"
)

const cloudflareUUID = "03e12345-6789-4abc-8def-0123456789ab"

func TestCloudflarePayloadNormalizeAndDuplicates(t *testing.T) {
	storage := newCloudflareStorage(t)
	biz := NewTunnelBiz(storage)
	created, err := biz.Create(model.TunnelPayload{
		Name: "DevSpace", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 3000, Status: "stopped",
		CloudflareIDMode: "auto",
		Cloudflare: &model.CloudflareTunnelConfig{
			TunnelName: "loris-devspace",
			Hostname:   "HTTPS://DevSpace.Example.com/",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Cloudflare == nil || created.Cloudflare.TunnelID != "" || created.Cloudflare.Hostname != "devspace.example.com" || len(created.JumperIDs) != 0 {
		t.Fatalf("created = %+v", created.Cloudflare)
	}
	if _, err := biz.Create(model.TunnelPayload{
		Name: "Other", Provider: model.ProviderCloudflare, Mode: "https",
		LocalHost: "127.0.0.1", LocalPort: 3001, Status: "stopped",
		CloudflareIDMode: "auto",
		Cloudflare:       &model.CloudflareTunnelConfig{TunnelName: "other", Hostname: "devspace.example.com"},
	}); err == nil || !strings.Contains(err.Error(), "Hostname is already used") {
		t.Fatalf("duplicate hostname err = %v", err)
	}
	bound, err := biz.Create(model.TunnelPayload{
		Name: "Bound", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 3002, Status: "stopped",
		CloudflareIDMode: "bind",
		Cloudflare:       &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, TunnelName: "existing", Hostname: "bound.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := biz.Create(model.TunnelPayload{
		Name: "Again", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 3003, Status: "stopped",
		CloudflareIDMode: "bind",
		Cloudflare:       &model.CloudflareTunnelConfig{TunnelID: strings.ToUpper(cloudflareUUID), Hostname: "again.example.com", TunnelName: "again"},
	}); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("duplicate id err = %v", err)
	}
	payload := model.TunnelPayload{
		Name: "Bound", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 3002, Status: "stopped",
		CloudflareIDMode: "bind",
		Cloudflare:       &model.CloudflareTunnelConfig{TunnelID: "11111111-1111-4111-8111-111111111111", Hostname: "bound.example.com", TunnelName: "existing"},
	}
	if _, err := biz.Update(bound.ID, payload); err == nil {
		t.Fatal("bound tunnel id was replaced")
	}
}

func TestCloudflareAutoCreateStartUsesSameID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.MkdirAll(filepath.Join(home, ".cloudflared"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeBizCert(t, filepath.Join(home, ".cloudflared"))
	logPath := filepath.Join(t.TempDir(), "args.txt")
	listener := listenLocal(t)
	port := listener.Addr().(*net.TCPAddr).Port
	biz := newCloudflareBiz(t, cloudflareScript(logPath, cloudflareUUID))
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" {
			t.Errorf("unexpected API path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone-1","name":"example.com"}],"result_info":{"total_pages":1}}`))
	}))
	t.Cleanup(api.Close)
	biz.cloudflare.ZoneAPI = api.URL
	created, err := biz.Create(model.TunnelPayload{
		Name: "DevSpace", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: port, Status: "stopped", AutoStart: true,
		CloudflareIDMode: "auto",
		Cloudflare:       &model.CloudflareTunnelConfig{TunnelName: "test-loris", Hostname: "test.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Cloudflare.TunnelID != "" {
		t.Fatal("save created a remote tunnel")
	}
	if _, err := biz.Toggle(created.ID, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	waitCloudflareStatus(t, biz, created.ID, "running")
	items, err := biz.List()
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Cloudflare.TunnelID != cloudflareUUID || items[0].PublicURL != "https://test.example.com" {
		t.Fatalf("running = %+v", items[0])
	}
	if _, err := biz.Toggle(created.ID, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	biz.startWG.Wait()
	if _, err := biz.Toggle(created.ID, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	waitCloudflareStatus(t, biz, created.ID, "running")
	log, _ := os.ReadFile(logPath)
	if strings.Count(string(log), "create") != 1 {
		t.Fatalf("create count in %s", log)
	}
	biz.Shutdown()
}

func TestCloudflareStartErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	biz := newCloudflareBiz(t, cloudflareScript(filepath.Join(t.TempDir(), "args.txt"), cloudflareUUID))
	listener := listenLocal(t)
	port := listener.Addr().(*net.TCPAddr).Port
	auth, err := biz.Create(model.TunnelPayload{
		Name: "Auth", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: port, Status: "stopped",
		CloudflareIDMode: "bind",
		Cloudflare:       &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, TunnelName: "auth", Hostname: "auth.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := biz.Toggle(auth.ID, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	waitCloudflareStatus(t, biz, auth.ID, "auth_required")
	if err := biz.Delete(auth.ID); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".cloudflared"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".cloudflared", "cert.pem"), []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing, err := biz.Create(model.TunnelPayload{
		Name: "Missing", Provider: model.ProviderCloudflare, Mode: "https",
		LocalHost: "127.0.0.1", LocalPort: port, Status: "stopped",
		CloudflareIDMode: "bind",
		Cloudflare:       &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, TunnelName: "missing", Hostname: "missing.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := biz.Toggle(missing.ID, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	waitCloudflareStatus(t, biz, missing.ID, "needs_setup")
	items, _ := biz.List()
	for _, item := range items {
		if item.ID == missing.ID && !strings.Contains(item.LastError, "credentials are missing") {
			t.Fatalf("missing credential error = %s", item.LastError)
		}
	}
}

func newCloudflareStorage(t *testing.T) *conf.Storage {
	t.Helper()
	storage, err := conf.NewStorage(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return storage
}

func newCloudflareBiz(t *testing.T, script string) *TunnelBiz {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cloudflared")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	biz := NewTunnelBiz(newCloudflareStorage(t))
	biz.cloudflare = cloudflared.NewClient(path)
	return biz
}

func cloudflareScript(logPath, id string) string {
	return `#!/bin/sh
echo "$@" >> "` + logPath + `"
while [ "$2" = "--origincert" ] || [ "$2" = "--config" ] || [ "$2" = "--credentials-file" ]; do first="$1"; shift 3; set -- "$first" "$@"; done
if [ "$1" = "--version" ]; then echo "cloudflared version 2026.1.0"; exit 0; fi
if [ "$1" = "tunnel" ] && [ "$2" = "list" ]; then
  echo '[{"id":"` + id + `","name":"test-loris","created_at":"2024-01-01T00:00:00Z","connections":[]}]'
  exit 0
fi
if [ "$1" = "tunnel" ] && [ "$2" = "create" ]; then
  mkdir -p "$HOME/.cloudflared"
  echo '{}' > "$HOME/.cloudflared/` + id + `.json"
  echo "Created tunnel $3 with id ` + id + `"
  exit 0
fi
if [ "$1" = "tunnel" ] && [ "$2" = "route" ]; then echo "Added CNAME"; exit 0; fi
if [ "$1" = "tunnel" ]; then
  echo "Registered tunnel connection connIndex=0"
  while true; do sleep 0.2; done
fi
exit 0
`
}

func listenLocal(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func waitCloudflareStatus(t *testing.T, tunnelBiz *TunnelBiz, id int, status string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err := tunnelBiz.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.ID == id && item.Status == status {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	items, _ := tunnelBiz.List()
	t.Fatalf("tunnel %d did not reach %s: %+v", id, status, items)
}

func TestCloudflareDeleteLinked(t *testing.T) {
	biz := newCloudflareBiz(t, cloudflareScript(filepath.Join(t.TempDir(), "args.txt"), cloudflareUUID))
	created, err := biz.Create(model.TunnelPayload{
		Name: "DevSpace", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 3000, Status: "stopped",
		CloudflareIDMode: "bind",
		Cloudflare:       &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, TunnelName: "dev", Hostname: "dev.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := biz.DeleteCloudflareResource(context.Background(), cloudflareUUID)
	if result.State != "linked" || result.TunnelName != created.Name {
		t.Fatalf("delete = %+v", result)
	}
}

func TestCloudflareAuthorizationRemovalGuards(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone-1","name":"example.com"}],"result_info":{"total_pages":1}}`))
	}))
	defer api.Close()
	b := newDNSBiz(t, api.URL, cloudflareScript(filepath.Join(t.TempDir(), "args"), cloudflareUUID))
	_, err := b.Create(model.TunnelPayload{Name: "site", Provider: model.ProviderCloudflare, Mode: "http", LocalHost: "127.0.0.1", LocalPort: 3000, CloudflareIDMode: "bind", Cloudflare: &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, Hostname: "dev.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	list := b.ListCloudflareAuthorizations(ctx, true)
	if len(list.Items) != 1 || list.Items[0].LinkedTunnels != 1 {
		t.Fatal(list)
	}
	if err := b.RemoveCloudflareAuthorization(ctx, "zone-1"); err == nil || err.Error() != "cloudflare_authorization_linked" {
		t.Fatal(err)
	}
	if _, err := b.storage.Update(func(cfg *conf.Config) error { cfg.Tunnels = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	task := deletionTask{OperationID: newOperationID(), Provider: model.ProviderCloudflare, Account: b.deletionAccount(ctx, model.ProviderCloudflare), DNS: []cloudflared.DNSRecord{{ID: "record", ZoneID: "zone-1"}}}
	if err := b.writeDeletionTask(task); err != nil {
		t.Fatal(err)
	}
	if err := b.RemoveCloudflareAuthorization(ctx, "zone-1"); err == nil || err.Error() != "cloudflare_authorization_cleanup_pending" {
		t.Fatal(err)
	}
	b.removeDeletionTask(task.OperationID)
	if err := b.RemoveCloudflareAuthorization(ctx, "zone-1"); err != nil {
		t.Fatal(err)
	}
	if list := b.ListCloudflareAuthorizations(ctx, true); len(list.Items) != 0 {
		t.Fatal("removed authorization returned", list)
	}
}
