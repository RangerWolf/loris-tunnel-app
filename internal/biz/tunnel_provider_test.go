package biz

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/model"
)

func TestDevTunnelRegionIsStoredOnlyForNewIDs(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	tunnelBiz := NewTunnelBiz(storage)
	created, err := tunnelBiz.Create(model.TunnelPayload{
		Name:      "web",
		Provider:  model.ProviderDevTunnel,
		Mode:      "http",
		LocalHost: "127.0.0.1",
		LocalPort: 3000,
		Status:    "stopped",
		DevTunnel: &model.DevTunnelConfig{Access: "private", Region: "USW3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.DevTunnel.Region != "usw3" {
		t.Fatalf("region = %q", created.DevTunnel.Region)
	}
	if _, err := tunnelBiz.Create(model.TunnelPayload{
		Name:      "bad",
		Provider:  model.ProviderDevTunnel,
		Mode:      "http",
		LocalHost: "127.0.0.1",
		LocalPort: 3000,
		Status:    "stopped",
		DevTunnel: &model.DevTunnelConfig{Access: "private", Region: "https://evil.example"},
	}); err == nil {
		t.Fatal("arbitrary region text was accepted")
	}
	bound, err := tunnelBiz.Create(model.TunnelPayload{
		Name:            "bound",
		Provider:        model.ProviderDevTunnel,
		Mode:            "http",
		LocalHost:       "127.0.0.1",
		LocalPort:       3001,
		Status:          "stopped",
		DevTunnelIDMode: "bind",
		DevTunnel:       &model.DevTunnelConfig{Access: "private", TunnelID: "abc123.usw3", Region: "euw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bound.DevTunnel.Region != "" || bound.DevTunnel.TunnelID != "abc123.usw3" {
		t.Fatalf("bound region = %+v", bound.DevTunnel)
	}
}

func TestDevTunnelCreateDoesNotRequireJumpers(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	tunnelBiz := NewTunnelBiz(storage)
	created, err := tunnelBiz.Create(model.TunnelPayload{
		Name:      "web",
		Provider:  model.ProviderDevTunnel,
		Mode:      "http",
		LocalHost: "127.0.0.1",
		LocalPort: 3000,
		Status:    "stopped",
		DevTunnel: &model.DevTunnelConfig{Access: "private"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ProviderName() != model.ProviderDevTunnel || len(created.JumperIDs) != 0 || created.DevTunnel == nil {
		t.Fatalf("unexpected dev tunnel: %+v", created)
	}
	if created.DevTunnel.Access != "private" || created.DevTunnel.Expiration != "30d" {
		t.Fatalf("defaults were not applied: %+v", created.DevTunnel)
	}
}

func TestDevTunnelExpirationIsSentToCLI(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli-args.txt")
	tunnelBiz := newDevTunnelBiz(t, devTunnelScriptRecording(logPath))
	created, err := tunnelBiz.Create(model.TunnelPayload{
		Name:      "web",
		Provider:  model.ProviderDevTunnel,
		Mode:      "http",
		LocalHost: "127.0.0.1",
		LocalPort: 3000,
		Status:    "stopped",
		DevTunnel: &model.DevTunnelConfig{Access: "private", Expiration: "12h"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.DevTunnel.Expiration != "12h" {
		t.Fatalf("stored expiration = %s", created.DevTunnel.Expiration)
	}
	if _, err := tunnelBiz.Create(model.TunnelPayload{
		Name:      "too-long",
		Provider:  model.ProviderDevTunnel,
		Mode:      "http",
		LocalHost: "127.0.0.1",
		LocalPort: 3000,
		Status:    "stopped",
		DevTunnel: &model.DevTunnelConfig{Access: "private", Expiration: "31d"},
	}); err == nil {
		t.Fatal("31d was accepted")
	}
	waitDevTunnelRunning(t, tunnelBiz, created.ID)
	if args := readCLIArgs(t, logPath); !strings.Contains(args, "create --expiration 12h") {
		t.Fatalf("create args = %q", args)
	}
	tunnelID := devTunnelID(t, tunnelBiz, created.ID)
	if _, err := tunnelBiz.Toggle(created.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}
	tunnelBiz.startWG.Wait()
	updated, err := tunnelBiz.Update(created.ID, model.TunnelPayload{
		Name:      "web",
		Provider:  model.ProviderDevTunnel,
		Mode:      "http",
		LocalHost: "127.0.0.1",
		LocalPort: 3000,
		Status:    "stopped",
		DevTunnel: &model.DevTunnelConfig{Access: "private", Expiration: "30d", TunnelID: tunnelID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DevTunnel.Expiration != "30d" || updated.DevTunnel.TunnelID != tunnelID {
		t.Fatalf("updated dev tunnel = %+v", updated.DevTunnel)
	}
	waitDevTunnelRunning(t, tunnelBiz, created.ID)
	args := readCLIArgs(t, logPath)
	if strings.Contains(args, "\nupdate ") || strings.HasPrefix(strings.TrimSpace(args), "update ") {
		t.Fatalf("existing tunnel expiration was pushed: %q", args)
	}
	if !strings.Contains(args, "show "+tunnelID+" -j") {
		t.Fatalf("show args = %q", args)
	}
}

func devTunnelID(t *testing.T, tunnelBiz *TunnelBiz, id int) string {
	t.Helper()
	listed, err := tunnelBiz.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed {
		if item.ID == id && item.DevTunnel != nil && item.DevTunnel.TunnelID != "" {
			return item.DevTunnel.TunnelID
		}
	}
	t.Fatal("dev tunnel id was not saved")
	return ""
}

func waitDevTunnelRunning(t *testing.T, tunnelBiz *TunnelBiz, id int) {
	t.Helper()
	if _, err := tunnelBiz.Toggle(id, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		listed, err := tunnelBiz.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range listed {
			if item.ID == id && item.Status == "running" {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("dev tunnel did not start")
}

func readCLIArgs(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDevTunnelAuthFailureDoesNotReserveSlot(t *testing.T) {
	tunnelBiz := newDevTunnelBiz(t, devTunnelScript("expired"))
	created := mustCreateDevTunnel(t, tunnelBiz)
	updated, err := tunnelBiz.Toggle(created.ID, func() int { return MaxRunningTunnels })
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "busy" {
		t.Fatalf("expected immediate busy, got %s", updated.Status)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		listed, listErr := tunnelBiz.List()
		if listErr != nil {
			t.Fatal(listErr)
		}
		if listed[0].Status == "auth_required" && tunnelBiz.RunningCount() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("auth failure left status=%s slots=%d", updated.Status, tunnelBiz.RunningCount())
}

func TestDevTunnelStartStopReleasesSlot(t *testing.T) {
	tunnelBiz := newDevTunnelBiz(t, devTunnelScript("ready"))
	created := mustCreateDevTunnel(t, tunnelBiz)
	updated, err := tunnelBiz.Toggle(created.ID, func() int { return MaxRunningTunnels })
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "busy" {
		t.Fatalf("expected busy start, got %s %s", updated.Status, updated.LastError)
	}
	deadline := time.Now().Add(4 * time.Second)
	var listed []model.Tunnel
	for time.Now().Before(deadline) {
		listed, err = tunnelBiz.List()
		if err != nil {
			t.Fatal(err)
		}
		if listed[0].Status == "running" && listed[0].PublicURL != "" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if listed[0].Status != "running" || listed[0].PublicURL == "" {
		t.Fatalf("host did not become ready: %+v", listed[0])
	}
	if _, err := tunnelBiz.Toggle(created.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}
	tunnelBiz.startWG.Wait()
	reopenedStorage, err := conf.NewStorage(tunnelBiz.storage.Path())
	if err != nil {
		t.Fatal(err)
	}
	reopened := NewTunnelBiz(reopenedStorage)
	cached, cacheErr := reopened.List()
	if cacheErr != nil {
		t.Fatal(cacheErr)
	}
	if len(cached) != 1 || cached[0].PublicURL == "" || cached[0].PublicURL != listed[0].PublicURL {
		t.Fatalf("last public URL missing after restart: %+v", cached)
	}
	payload := model.TunnelPayload{Name: cached[0].Name, Provider: model.ProviderDevTunnel, Mode: cached[0].Mode, LocalHost: cached[0].LocalHost, LocalPort: cached[0].LocalPort, DevTunnel: cached[0].DevTunnel, DevTunnelIDMode: "bind"}
	if _, err := reopened.Update(created.ID, payload); err != nil {
		t.Fatal(err)
	}
	preserved, _ := reopened.List()
	if preserved[0].PublicURL != cached[0].PublicURL {
		t.Fatal("editing the same tunnel dropped the cached URL")
	}
	if tunnelBiz.RunningCount() != 0 {
		t.Fatalf("stop left %d slots", tunnelBiz.RunningCount())
	}
}

func newDevTunnelBiz(t *testing.T, script string) *TunnelBiz {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devtunnel")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	storage, err := conf.NewStorage(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	tunnelBiz := NewTunnelBiz(storage)
	tunnelBiz.dev = devtunnel.NewClient(path)
	return tunnelBiz
}

func mustCreateDevTunnel(t *testing.T, tunnelBiz *TunnelBiz) model.Tunnel {
	t.Helper()
	created, err := tunnelBiz.Create(model.TunnelPayload{
		Name:      "web",
		Provider:  model.ProviderDevTunnel,
		Mode:      "http",
		LocalHost: "127.0.0.1",
		LocalPort: 3000,
		Status:    "stopped",
		DevTunnel: &model.DevTunnelConfig{Access: "private"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func devTunnelScriptRecording(logPath string) string {
	return `#!/bin/sh
echo "$@" >> "` + logPath + `"
case "$1" in
  --version)
    echo "Tunnel CLI version: 1.0.2115"
    ;;
  user)
    echo '{"login":"octo","provider":"github"}'
    ;;
  create)
    echo '{"tunnelId":"abc123","clusterId":"usw3"}'
    ;;
  show)
    echo '{"tunnelId":"abc123","clusterId":"usw3","ports":[{"portNumber":3000,"protocol":"http"}],"hostConnections":0}'
    ;;
  port)
    exit 0
    ;;
  host)
    echo "Connect via browser: https://abc123-3000.usw3.devtunnels.ms"
    echo "Ready to accept connections for tunnel: abc123.usw3"
    while true; do sleep 0.2; done
    ;;
esac
exit 0
`
}

func TestDevTunnelIDBindingAndRemoteDelete(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli-args.txt")
	tunnelBiz := newDevTunnelBiz(t, devTunnelResourceScript(logPath))
	created := mustCreateDevTunnel(t, tunnelBiz)
	waitDevTunnelRunning(t, tunnelBiz, created.ID)
	if _, err := tunnelBiz.Toggle(created.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}
	tunnelBiz.startWG.Wait()
	tunnelID := devTunnelID(t, tunnelBiz, created.ID)

	if _, err := tunnelBiz.Create(model.TunnelPayload{
		Name:            "other",
		Provider:        model.ProviderDevTunnel,
		Mode:            "http",
		LocalHost:       "127.0.0.1",
		LocalPort:       3000,
		Status:          "stopped",
		DevTunnelIDMode: "bind",
		DevTunnel:       &model.DevTunnelConfig{Access: "private", TunnelID: "abc123"},
	}); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("duplicate bind err=%v", err)
	}

	linked := tunnelBiz.DeleteDevTunnelResource(context.Background(), tunnelID)
	if linked.State != "linked" {
		t.Fatalf("linked delete = %+v", linked)
	}
	if strings.Contains(readCLIArgs(t, logPath), "\ndelete ") {
		t.Fatal("linked id was deleted remotely")
	}

	if err := tunnelBiz.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readCLIArgs(t, logPath), "\ndelete ") {
		t.Fatal("local delete removed the remote tunnel")
	}
	deleted := tunnelBiz.DeleteDevTunnelResource(context.Background(), tunnelID)
	if deleted.State != "deleted" {
		t.Fatalf("remote delete = %+v", deleted)
	}
	if !strings.Contains(readCLIArgs(t, logPath), "delete "+tunnelID+" -f") {
		t.Fatalf("delete args = %q", readCLIArgs(t, logPath))
	}
}

func TestDevTunnelExistingIDPortRules(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli-args.txt")
	tunnelBiz := newDevTunnelBiz(t, devTunnelResourceScript(logPath))

	missing := mustCreateBoundDevTunnel(t, tunnelBiz, "gone", "missing.usw3", 3000, "http")
	if _, err := tunnelBiz.Toggle(missing.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}
	waitDevTunnelStatus(t, tunnelBiz, missing.ID, "error")
	if got := devTunnelID(t, tunnelBiz, missing.ID); got != "missing.usw3" {
		t.Fatalf("missing id was cleared: %s", got)
	}
	if strings.Contains(readCLIArgs(t, logPath), "\ncreate ") || strings.HasPrefix(readCLIArgs(t, logPath), "create ") {
		t.Fatal("missing id created a replacement")
	}

	multi := mustCreateBoundDevTunnel(t, tunnelBiz, "multi", "multi.usw3", 3000, "http")
	if _, err := tunnelBiz.Toggle(multi.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}
	waitDevTunnelStatus(t, tunnelBiz, multi.ID, "running")
	if _, err := tunnelBiz.Toggle(multi.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}

	mismatch := mustCreateBoundDevTunnel(t, tunnelBiz, "mismatch", "one.usw3", 3000, "http")
	if _, err := tunnelBiz.Toggle(mismatch.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}
	waitDevTunnelStatus(t, tunnelBiz, mismatch.ID, "running")
	if _, err := tunnelBiz.Toggle(mismatch.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}

	empty := mustCreateBoundDevTunnel(t, tunnelBiz, "empty", "empty.usw3", 8080, "https")
	waitDevTunnelRunning(t, tunnelBiz, empty.ID)
	if !strings.Contains(readCLIArgs(t, logPath), "port create empty.usw3 -p 8080 --protocol https") {
		t.Fatalf("port create args = %q", readCLIArgs(t, logPath))
	}

	cleared, err := tunnelBiz.Update(empty.ID, model.TunnelPayload{
		Name:            "empty",
		Provider:        model.ProviderDevTunnel,
		Mode:            "https",
		LocalHost:       "127.0.0.1",
		LocalPort:       8080,
		Status:          "stopped",
		DevTunnelIDMode: "auto",
		DevTunnel:       &model.DevTunnelConfig{Access: "private", TunnelID: "empty.usw3"},
	})
	if err == nil {
		t.Fatal("running tunnel changed its id")
	}
	_ = cleared
	if _, err := tunnelBiz.Toggle(empty.ID, func() int { return MaxRunningTunnels }); err != nil {
		t.Fatal(err)
	}
	tunnelBiz.startWG.Wait()
	_, err = tunnelBiz.Update(empty.ID, model.TunnelPayload{
		Name:            "empty",
		Provider:        model.ProviderDevTunnel,
		Mode:            "https",
		LocalHost:       "127.0.0.1",
		LocalPort:       8080,
		Status:          "stopped",
		DevTunnelIDMode: "auto",
		DevTunnel:       &model.DevTunnelConfig{Access: "private", TunnelID: "empty.usw3"},
	})
	if err == nil {
		t.Fatal("stopped tunnel cleared its bound id")
	}
	cfg, err := tunnelBiz.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range cfg.Tunnels {
		if item.ID == empty.ID && (item.DevTunnel == nil || item.DevTunnel.TunnelID != "empty.usw3") {
			t.Fatal("rejected update changed the binding")
		}
	}
}

func TestDevTunnelConcurrentBind(t *testing.T) {
	tunnelBiz := newDevTunnelBiz(t, devTunnelScript("ready"))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		name := "web"
		if i == 1 {
			name = "web-2"
		}
		go func(name string) {
			defer wg.Done()
			_, err := tunnelBiz.Create(model.TunnelPayload{
				Name:            name,
				Provider:        model.ProviderDevTunnel,
				Mode:            "http",
				LocalHost:       "127.0.0.1",
				LocalPort:       3000,
				Status:          "stopped",
				DevTunnelIDMode: "bind",
				DevTunnel:       &model.DevTunnelConfig{Access: "private", TunnelID: "abc123.usw3"},
			})
			errs <- err
		}(name)
	}
	wg.Wait()
	close(errs)
	failures := 0
	for err := range errs {
		if err != nil {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("failures=%d", failures)
	}
}

func mustCreateBoundDevTunnel(t *testing.T, tunnelBiz *TunnelBiz, name, tunnelID string, port int, protocol string) model.Tunnel {
	t.Helper()
	created, err := tunnelBiz.Create(model.TunnelPayload{
		Name:            name,
		Provider:        model.ProviderDevTunnel,
		Mode:            protocol,
		LocalHost:       "127.0.0.1",
		LocalPort:       port,
		Status:          "stopped",
		DevTunnelIDMode: "bind",
		DevTunnel:       &model.DevTunnelConfig{Access: "private", TunnelID: tunnelID},
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func waitDevTunnelStatus(t *testing.T, tunnelBiz *TunnelBiz, id int, status string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		listed, err := tunnelBiz.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range listed {
			if item.ID == id && item.Status == status {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("tunnel %d did not reach %s", id, status)
}

func devTunnelResourceScript(logPath string) string {
	return `#!/bin/sh
echo "$@" >> "` + logPath + `"
case "$1" in
  --version)
    echo "Tunnel CLI version: 1.0.2115"
    ;;
  user)
    echo '{"login":"octo","provider":"github"}'
    ;;
  create)
    echo '{"tunnelId":"abc123","clusterId":"usw3"}'
    ;;
  show)
    case "$2" in
      missing.usw3)
        echo "Tunnel not found" >&2
        exit 1
        ;;
      multi.usw3)
        if [ -f "` + logPath + `.deleted-3001" ]; then
          echo '{"tunnelId":"multi","clusterId":"usw3","ports":[{"portNumber":3000,"protocol":"http"}],"hostConnections":0}'
        else
        echo '{"tunnelId":"multi","clusterId":"usw3","ports":[{"portNumber":3000,"protocol":"http"},{"portNumber":3001,"protocol":"https"}],"hostConnections":0}'
        fi
        ;;
      one.usw3)
        if [ -f "` + logPath + `.created-3000" ]; then
          echo '{"tunnelId":"one","clusterId":"usw3","ports":[{"portNumber":3000,"protocol":"http"}],"hostConnections":0}'
        else
        echo '{"tunnelId":"one","clusterId":"usw3","ports":[{"portNumber":9090,"protocol":"http"}],"hostConnections":0}'
        fi
        ;;
      empty.usw3)
        if [ -f "` + logPath + `.created-8080" ]; then
          echo '{"tunnelId":"empty","clusterId":"usw3","ports":[{"portNumber":8080,"protocol":"https"}],"hostConnections":0}'
        else
        echo '{"tunnelId":"empty","clusterId":"usw3","ports":[],"hostConnections":0}'
        fi
        ;;
      *)
        echo '{"tunnelId":"abc123","clusterId":"usw3","ports":[{"portNumber":3000,"protocol":"http"}],"hostConnections":0}'
        ;;
    esac
    ;;
  port)
    if [ "$2" = "delete" ]; then touch "` + logPath + `.deleted-$5"; fi
    if [ "$2" = "create" ]; then touch "` + logPath + `.created-$5"; fi
    exit 0
    ;;
  delete)
    exit 0
    ;;
  host)
    tunnel="$2"
    shift 2
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "-p" ]; then shift; echo "Connect via browser: https://abc123-$1.usw3.devtunnels.ms"; fi
      shift
    done
    echo "Ready to accept connections for tunnel: $tunnel"
    while true; do sleep 0.2; done
    ;;
esac
exit 0
`
}

func devTunnelScript(mode string) string {
	account := `{"login":"octo","provider":"github"}`
	if mode == "expired" {
		account = `{"status":"Login token expired"}`
	}
	return `#!/bin/sh
case "$1" in
  --version)
    echo "Tunnel CLI version: 1.0.2115"
    ;;
  user)
    echo '` + account + `'
    ;;
  create)
    echo '{"tunnelId":"abc123"}'
    ;;
  show)
    echo '{"tunnelId":"abc123","ports":[{"portNumber":3000,"protocol":"http"}]}'
    ;;
  port)
    exit 0
    ;;
  host)
    echo "Connect via browser: https://abc123-3000.devtunnels.ms"
    echo "Ready to accept connections for tunnel: abc123"
    while true; do sleep 0.2; done
    ;;
esac
exit 0
`
}

func TestDevTunnelUpdateCannotReplaceBoundID(t *testing.T) {
	storage, err := conf.NewStorage(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	b := NewTunnelBiz(storage)
	payload := model.TunnelPayload{
		Name: "web", Provider: model.ProviderDevTunnel, Mode: "http",
		LocalHost: "127.0.0.1", LocalPort: 8080, Status: "stopped",
		DevTunnelIDMode: "bind", DevTunnel: &model.DevTunnelConfig{TunnelID: "original.usw3", Access: "private"},
	}
	created, err := b.Create(payload)
	if err != nil {
		t.Fatal(err)
	}
	payload.Name = "renamed"
	if _, err := b.Update(created.ID, payload); err != nil {
		t.Fatalf("ordinary edit: %v", err)
	}
	payload.DevTunnel = &model.DevTunnelConfig{TunnelID: "different.usw3", Access: "private"}
	if _, err := b.Update(created.ID, payload); err == nil {
		t.Fatal("replacement accepted")
	}
	payload.DevTunnelIDMode = "auto"
	if _, err := b.Update(created.ID, payload); err == nil {
		t.Fatal("clearing bound ID accepted")
	}
	payload.DevTunnelIDMode = "bind"
	payload.DevTunnel = &model.DevTunnelConfig{TunnelID: "original.usw3", Access: "private"}
	updated, err := b.Update(created.ID, payload)
	if err != nil || updated.DevTunnel.TunnelID != "original.usw3" {
		t.Fatalf("binding changed: %+v, %v", updated, err)
	}
}

func TestDevTunnelStaleHostCountDoesNotBlockStartup(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprint("hostFails=", fails), func(t *testing.T) {
			script := strings.ReplaceAll(devTunnelScriptRecording(filepath.Join(t.TempDir(), "args.log")), `"hostConnections":0`, `"hostConnections":1`)
			if fails {
				script = strings.Replace(script, `echo "Connect via browser: https://abc123-3000.usw3.devtunnels.ms"`, "echo 'Tunnel is already connected to another host' >&2; exit 1", 1)
			}
			b := newDevTunnelBiz(t, script)
			created, err := b.Create(model.TunnelPayload{
				Name: "existing", Provider: model.ProviderDevTunnel, Mode: "http", LocalHost: "127.0.0.1", LocalPort: 3000, Status: "stopped",
				DevTunnelIDMode: "bind", DevTunnel: &model.DevTunnelConfig{TunnelID: "abc123.usw3", Access: "private"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Toggle(created.ID, func() int { return 0 }); err != nil {
				t.Fatal(err)
			}
			b.startWG.Wait()
			items, err := b.List()
			if err != nil {
				t.Fatal(err)
			}
			if fails {
				if items[0].Status != "error" || !strings.Contains(items[0].LastError, "already connected to another host") {
					t.Fatalf("CLI failure lost: %+v", items[0])
				}
				if b.RunningCount() != 0 {
					t.Fatal("failed host reserved a slot")
				}
			} else {
				if items[0].Status != "running" || items[0].PublicURL == "" {
					t.Fatalf("stale count blocked start: %+v", items[0])
				}
				if _, err := b.Toggle(created.ID, func() int { return 0 }); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDevTunnelStopCancelsSlowPreparation(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "preflight")
	script := strings.Replace(devTunnelScript("ready"), "  user)", fmt.Sprintf(`  user)
    echo started > "%s"
    sleep 30`, marker), 1)
	b := newDevTunnelBiz(t, script)
	created := mustCreateDevTunnel(t, b)
	if _, err := b.Toggle(created.ID, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			b.Shutdown()
			t.Fatal("preflight did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := b.Toggle(created.ID, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { b.startWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stopping did not cancel preparation")
	}
	items, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Status != "stopped" || b.RunningCount() != 0 {
		t.Fatalf("canceled start changed state: %+v", items[0])
	}
}

func TestDevTunnelSharedIDCreationRejectedWithoutDisturbingOwner(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args.log")
	b := newDevTunnelBiz(t, devTunnelResourceScript(logPath))
	defer b.Shutdown()
	first := mustCreateBoundDevTunnel(t, b, "first-service", "abc123.usw3", 3000, "auto")
	waitDevTunnelRunning(t, b, first.ID)
	_, err := b.Create(model.TunnelPayload{Name: "second", Provider: model.ProviderDevTunnel, Mode: "auto", LocalHost: "127.0.0.1", LocalPort: 3001, Status: "stopped", DevTunnelIDMode: "bind", DevTunnel: &model.DevTunnelConfig{Access: "private", TunnelID: "abc123.usw3"}})
	if err == nil || !strings.Contains(err.Error(), "first-service") {
		t.Fatal("shared ID accepted", err)
	}
	waitDevTunnelStatus(t, b, first.ID, "running")
	if strings.Contains(readCLIArgs(t, logPath), "port delete") {
		t.Fatal("failed bind changed remote ports")
	}
}
