package biz

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/model"
)

func TestDeleteOneDevServiceKeepsTheSharedTunnel(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	biz := newDevTunnelBiz(t, devDeleteScript(logPath, ""))
	first := mustCreateDevTunnel(t, biz)
	second := mustCreateDevTunnel(t, biz)
	setDevBinding(t, biz, first.ID, "api", "abc123.usw3", 3000, 42001)
	setDevBinding(t, biz, second.ID, "web", "abc123.usw3", 8080, 42002)

	result := biz.DeleteTunnelWithOptions(context.Background(), first.ID, true)
	if result.Status != DeletionCompleted || !result.LocalDeleted {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Items) != 1 || result.Items[0].Target != "42001" || result.Items[0].Status != CleanupDeleted {
		t.Fatalf("items = %+v", result.Items)
	}
	log, _ := os.ReadFile(logPath)
	text := string(log)
	if !strings.Contains(text, "port delete abc123.usw3 -p 42001") || strings.Contains(text, "-p 42002") {
		t.Fatalf("cli log = %s", text)
	}
	left, err := biz.List()
	if err != nil || len(left) != 1 || left[0].ID != second.ID || left[0].DevTunnel.TunnelPort != 42002 {
		t.Fatalf("remaining = %+v %v", left, err)
	}
}

func TestDeleteUsesPersistedTunnelPort(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	biz := newDevTunnelBiz(t, devDeleteScript(logPath, ""))
	created := mustCreateDevTunnel(t, biz)
	setDevBinding(t, biz, created.ID, "api", "abc123.usw3", 3000, 42001)
	result := biz.DeleteTunnelWithOptions(context.Background(), created.ID, true)
	if result.Status != DeletionCompleted {
		t.Fatalf("result = %+v", result)
	}
	log, _ := os.ReadFile(logPath)
	if !strings.Contains(string(log), "-p 42001") || strings.Contains(string(log), "-p 3000") {
		t.Fatalf("deleted the wrong port: %s", log)
	}
}

func TestUnknownDevPortIsNotGuessed(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	biz := newDevTunnelBiz(t, devDeleteScript(logPath, `{"tunnelId":"abc123","clusterId":"usw3","ports":[{"portNumber":42001},{"portNumber":42002}]}`))
	created := mustCreateDevTunnel(t, biz)
	setDevBinding(t, biz, created.ID, "api", "abc123.usw3", 3000, 0)
	result := biz.DeleteTunnelWithOptions(context.Background(), created.ID, true)
	if result.Status != DeletionFailed || result.ErrorCategory != "port_unknown" || result.LocalDeleted {
		t.Fatalf("result = %+v", result)
	}
	log, _ := os.ReadFile(logPath)
	if strings.Contains(string(log), "port delete") {
		t.Fatalf("guessed a port delete: %s", log)
	}
	left, _ := biz.List()
	if len(left) != 1 {
		t.Fatalf("record removed: %+v", left)
	}
}

func TestSharedDevPortIsPreserved(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	biz := newDevTunnelBiz(t, devDeleteScript(logPath, ""))
	first := mustCreateDevTunnel(t, biz)
	second := mustCreateDevTunnel(t, biz)
	setDevBinding(t, biz, first.ID, "api", "abc123.usw3", 3000, 42001)
	setDevBinding(t, biz, second.ID, "web", "abc123.usw3", 8080, 42001)
	result := biz.DeleteTunnelWithOptions(context.Background(), first.ID, true)
	if result.Status != DeletionCompleted || result.Items[0].Status != CleanupPreserved {
		t.Fatalf("result = %+v", result)
	}
	log, _ := os.ReadFile(logPath)
	if strings.Contains(string(log), "port delete") {
		t.Fatalf("deleted a shared port: %s", log)
	}
}

func TestCloudflareServiceDeletesOnlyMatchingCNAME(t *testing.T) {
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone-1","name":"example.com"}],"result_info":{"total_pages":1}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records/") && strings.HasSuffix(r.URL.Path, "/dns-1"):
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"dns-1","type":"CNAME","name":"test.example.com","content":"03e12345-6789-4abc-8def-0123456789ab.cfargotunnel.com"}}`))
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			name := r.URL.Query().Get("name")
			body := `{"success":true,"result":[],"result_info":{"total_pages":1}}`
			if name == "test.example.com" {
				body = `{"success":true,"result":[
					{"id":"dns-1","type":"CNAME","name":"test.example.com","content":"03e12345-6789-4abc-8def-0123456789ab.cfargotunnel.com"},
					{"id":"dns-a","type":"A","name":"test.example.com","content":"1.2.3.4"},
					{"id":"dns-other","type":"CNAME","name":"other.example.com","content":"03e12345-6789-4abc-8def-0123456789ab.cfargotunnel.com"}
				],"result_info":{"total_pages":1}}`
			}
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(server.Close)
	biz := newDNSBiz(t, server.URL, "#!/bin/sh\nexit 0\n")
	created, err := biz.Create(model.TunnelPayload{
		Name: "site", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 3000, Status: "stopped",
		Cloudflare: &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, Hostname: "test.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := biz.DeleteTunnelWithOptions(context.Background(), created.ID, true)
	if result.Status != DeletionCompleted || !result.LocalDeleted {
		t.Fatalf("result = %+v", result)
	}
	if len(deleted) != 1 || !strings.Contains(deleted[0], "dns-1") {
		t.Fatalf("deleted = %v", deleted)
	}
}

func TestDNSChangedAllowsLocalDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/zones" {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone-1","name":"example.com"}],"result_info":{"total_pages":1}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/dns_records/") {
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"dns-1","type":"CNAME","name":"test.example.com","content":"other.example.net"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"dns-1","type":"CNAME","name":"test.example.com","content":"03e12345-6789-4abc-8def-0123456789ab.cfargotunnel.com"}],"result_info":{"total_pages":1}}`))
	}))
	t.Cleanup(server.Close)
	biz := newDNSBiz(t, server.URL, "#!/bin/sh\nexit 0\n")
	created, err := biz.Create(model.TunnelPayload{
		Name: "site", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 3000, Status: "stopped",
		Cloudflare: &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, Hostname: "test.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := biz.DeleteTunnelWithOptions(context.Background(), created.ID, true)
	if result.Status != DeletionCompleted || !result.LocalDeleted || result.ErrorCategory != "dns_changed" {
		t.Fatalf("result = %+v", result)
	}
	if result.Items[0].Status != CleanupPreserved {
		t.Fatalf("items = %+v", result.Items)
	}
}

func TestProviderDeleteBlockedWhileLinked(t *testing.T) {
	biz := newDevTunnelBiz(t, devDeleteScript(filepath.Join(t.TempDir(), "cli.log"), ""))
	created := mustCreateDevTunnel(t, biz)
	setDevBinding(t, biz, created.ID, "api", "abc123.usw3", 3000, 42001)
	preview := biz.PreviewProviderResourceDeletion(context.Background(), model.ProviderDevTunnel, "abc123.usw3")
	if !preview.Blocked || len(preview.LinkedNames) != 1 || preview.LinkedNames[0] != "api" {
		t.Fatalf("preview = %+v", preview)
	}
	result := biz.DeleteProviderResourceWithOptions(context.Background(), model.ProviderDevTunnel, "abc123", false)
	if result.Status != DeletionBlocked || result.ErrorCategory != "linked" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRemoteMissingPermissionAndTimeout(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	biz := newDevTunnelBiz(t, "#!/bin/sh\necho \"$@\" >> \""+logPath+"\"\necho 'tunnel not found' >&2\nexit 1\n")
	created := mustCreateDevTunnel(t, biz)
	setDevBinding(t, biz, created.ID, "api", "abc123.usw3", 3000, 42001)
	missing := biz.DeleteTunnelWithOptions(context.Background(), created.ID, true)
	if missing.Status != DeletionCompleted || !missing.LocalDeleted {
		t.Fatalf("missing = %+v", missing)
	}

	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer denied.Close()
	deniedBiz := newDNSBiz(t, denied.URL, "#!/bin/sh\nexit 0\n")
	service, err := deniedBiz.Create(model.TunnelPayload{
		Name: "site", Provider: model.ProviderCloudflare, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 3000, Status: "stopped",
		Cloudflare: &model.CloudflareTunnelConfig{TunnelID: cloudflareUUID, Hostname: "test.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	permission := deniedBiz.DeleteTunnelWithOptions(context.Background(), service.ID, true)
	if permission.Status != DeletionFailed || permission.ErrorCategory != "dns_permission" || permission.LocalDeleted {
		t.Fatalf("permission = %+v", permission)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	timeoutBiz := newDevTunnelBiz(t, devDeleteScript(filepath.Join(t.TempDir(), "timeout.log"), ""))
	again := mustCreateDevTunnel(t, timeoutBiz)
	setDevBinding(t, timeoutBiz, again.ID, "api", "abc123.usw3", 3000, 42001)
	timed := timeoutBiz.DeleteTunnelWithOptions(ctx, again.ID, true)
	if timed.Status == DeletionCompleted || timed.LocalDeleted {
		t.Fatalf("timeout = %+v", timed)
	}
}

func TestPartialDNSRetryAfterRestart(t *testing.T) {
	var mu sync.Mutex
	failSecond := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/zones" {
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"zone-1","name":"example.com"}],"result_info":{"total_pages":1}}`))
			return
		}
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "dns-2") {
			mu.Lock()
			fail := failSecond
			mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		if strings.Contains(r.URL.RawQuery, "content=") {
			_, _ = w.Write([]byte(`{"success":true,"result":[
				{"id":"dns-1","type":"CNAME","name":"a.example.com","content":"03e12345-6789-4abc-8def-0123456789ab.cfargotunnel.com"},
				{"id":"dns-2","type":"CNAME","name":"b.example.com","content":"03e12345-6789-4abc-8def-0123456789ab.cfargotunnel.com"}
			],"result_info":{"total_pages":1}}`))
			return
		}
		id := "dns-1"
		name := "a.example.com"
		if strings.HasSuffix(r.URL.Path, "dns-2") {
			id = "dns-2"
			name = "b.example.com"
		}
		_, _ = w.Write([]byte(`{"success":true,"result":{"id":"` + id + `","type":"CNAME","name":"` + name + `","content":"03e12345-6789-4abc-8def-0123456789ab.cfargotunnel.com"}}`))
	}))
	t.Cleanup(server.Close)
	home := t.TempDir()
	writeBizCert(t, home)
	biz := newCloudflareBiz(t, "#!/bin/sh\nexit 0\n")
	biz.cloudflare.ConfigHome = home
	biz.cloudflare.ZoneAPI = server.URL
	result := biz.DeleteProviderResourceWithOptions(context.Background(), model.ProviderCloudflare, cloudflareUUID, true)
	if result.Status != DeletionPartial || !result.RemoteIDDeleted || result.OperationID == "" {
		t.Fatalf("partial = %+v", result)
	}
	reopened, err := conf.NewStorage(biz.storage.Path())
	if err != nil {
		t.Fatal(err)
	}
	next := NewTunnelBiz(reopened)
	next.cloudflare = biz.cloudflare
	mu.Lock()
	failSecond = false
	mu.Unlock()
	retried := next.RetryDeletionCleanup(context.Background(), result.OperationID)
	if retried.Status != DeletionCompleted {
		t.Fatalf("retry = %+v", retried)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(biz.storage.Path()), "deletion-tasks", result.OperationID+".json")); !os.IsNotExist(err) {
		t.Fatal("task file kept after success")
	}
}

func TestLocalWriteFailureRetryDoesNotDeleteOtherServices(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	biz := newDevTunnelBiz(t, devDeleteScript(logPath, ""))
	first := mustCreateDevTunnel(t, biz)
	second := mustCreateDevTunnel(t, biz)
	setDevBinding(t, biz, first.ID, "api", "abc123.usw3", 3000, 42001)
	setDevBinding(t, biz, second.ID, "web", "other.usw3", 8080, 43000)
	original, err := os.ReadFile(biz.storage.Path())
	if err != nil {
		t.Fatal(err)
	}
	biz.beforeForgetRecord = func() {
		_ = os.Remove(biz.storage.Path())
		_ = os.Mkdir(biz.storage.Path(), 0o700)
	}
	result := biz.DeleteTunnelWithOptions(context.Background(), first.ID, true)
	if result.Status != DeletionFailed || result.ErrorCategory != "local_write_failed" || result.LocalDeleted {
		t.Fatalf("result = %+v", result)
	}
	_ = os.RemoveAll(biz.storage.Path())
	if err := os.WriteFile(biz.storage.Path(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	biz.beforeForgetRecord = nil
	retried := biz.RetryDeletionCleanup(context.Background(), result.OperationID)
	if retried.Status != DeletionCompleted || !retried.LocalDeleted {
		t.Fatalf("retry = %+v", retried)
	}
	left, _ := biz.List()
	if len(left) != 1 || left[0].ID != second.ID {
		t.Fatalf("remaining = %+v", left)
	}
}

func TestDuplicateDeleteDoesNotDoubleExecute(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	biz := newDevTunnelBiz(t, "#!/bin/sh\nif [ \"$1\" = \"port\" ]; then\n  exit 0\nfi\nexit 0\n")
	created := mustCreateDevTunnel(t, biz)
	setDevBinding(t, biz, created.ID, "api", "abc123.usw3", 3000, 42001)
	biz.beforeForgetRecord = func() {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	}
	var first, second DeletionResult
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		first = biz.DeleteTunnelWithOptions(context.Background(), created.ID, true)
	}()
	<-started
	go func() {
		defer wg.Done()
		second = biz.DeleteTunnelWithOptions(context.Background(), created.ID, true)
	}()
	close(release)
	wg.Wait()
	blocked := 0
	completed := 0
	for _, result := range []DeletionResult{first, second} {
		if result.Status == DeletionBlocked && result.ErrorCategory == "in_progress" {
			blocked++
		}
		if result.Status == DeletionCompleted {
			completed++
		}
	}
	if blocked != 1 || completed != 1 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func setDevBinding(t *testing.T, biz *TunnelBiz, id int, name, tunnelID string, localPort, tunnelPort int) {
	t.Helper()
	_, err := biz.storage.Update(func(cfg *conf.Config) error {
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID != id || cfg.Tunnels[i].DevTunnel == nil {
				continue
			}
			cfg.Tunnels[i].Name = name
			cfg.Tunnels[i].LocalPort = localPort
			cfg.Tunnels[i].DevTunnel.LocalHost = "127.0.0.1"
			cfg.Tunnels[i].DevTunnel.LocalPort = localPort
			cfg.Tunnels[i].DevTunnel.TunnelID = tunnelID
			cfg.Tunnels[i].DevTunnel.TunnelPort = tunnelPort
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func devDeleteScript(logPath, show string) string {
	if show == "" {
		show = `{"tunnelId":"abc123","clusterId":"usw3","ports":[{"portNumber":42001}]}`
	}
	return "#!/bin/sh\necho \"$@\" >> \"" + logPath + "\"\nif [ \"$1\" = \"show\" ]; then echo '" + show + "'; exit 0; fi\nexit 0\n"
}

func newDNSBiz(t *testing.T, api, script string) *TunnelBiz {
	t.Helper()
	home := t.TempDir()
	writeBizCert(t, home)
	biz := newCloudflareBiz(t, script)
	biz.cloudflare.ConfigHome = home
	biz.cloudflare.ZoneAPI = api
	return biz
}

func writeBizCert(t *testing.T, dir string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"zoneID": "zone-1", "apiToken": "token", "accountID": "account"})
	if err != nil {
		t.Fatal(err)
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "ARGO TUNNEL TOKEN", Bytes: body})
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
