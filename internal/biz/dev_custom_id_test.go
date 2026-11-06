package biz

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

func devCustomPayload(name, mode string, cfg model.DevTunnelConfig) model.TunnelPayload {
	return model.TunnelPayload{Name: name, Provider: model.ProviderDevTunnel, Mode: "http", LocalHost: "127.0.0.1", LocalPort: 8080, Status: "stopped", DevTunnelIDMode: mode, DevTunnel: &cfg}
}

func baseDevConfig() model.DevTunnelConfig {
	return model.DevTunnelConfig{Access: "private", Expiration: "30d", Protocol: "http"}
}

func TestDevTunnelCustomModeInvariants(t *testing.T) {
	b := metadataCacheBiz(t, t.TempDir())

	created, err := b.Create(devCustomPayload("first", "custom", model.DevTunnelConfig{TunnelName: " Web-Dev-App ", Access: "private", Expiration: "30d"}))
	if err != nil {
		t.Fatal(err)
	}
	if created.DevTunnel.TunnelName != "web-dev-app" || created.DevTunnel.TunnelID != "" {
		t.Fatalf("stored = %+v", created.DevTunnel)
	}

	if _, err := b.Create(devCustomPayload("with-id", "custom", model.DevTunnelConfig{TunnelName: "web-app", TunnelID: "abc.usw3", Access: "private"})); err == nil {
		t.Fatal("custom with existing id was accepted")
	}
	if _, err := b.Create(devCustomPayload("bad-name", "custom", model.DevTunnelConfig{TunnelName: "my_project", Access: "private"})); err == nil {
		t.Fatal("invalid name was accepted")
	}
	if _, err := b.Create(devCustomPayload("short", "custom", model.DevTunnelConfig{TunnelName: "ab", Access: "private"})); err == nil {
		t.Fatal("short name was accepted")
	}

	auto, err := b.Create(devCustomPayload("auto", "auto", model.DevTunnelConfig{TunnelName: "web-app", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	if auto.DevTunnel.TunnelName != "" || auto.DevTunnel.TunnelID != "" {
		t.Fatalf("auto kept leftovers: %+v", auto.DevTunnel)
	}

	bind, err := b.Create(devCustomPayload("bind", "bind", model.DevTunnelConfig{TunnelID: "abc.usw3", TunnelName: "web-app", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	if bind.DevTunnel.TunnelName != "" || bind.DevTunnel.TunnelID != "abc.usw3" {
		t.Fatalf("bind kept leftovers: %+v", bind.DevTunnel)
	}

	// Legacy payloads without a mode keep the existing inherit behavior and
	// never keep a pending name next to an existing id.
	legacy, err := b.Create(devCustomPayload("legacy", "", model.DevTunnelConfig{TunnelID: "old.usw3", TunnelName: "web-app", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.DevTunnel.TunnelID != "old.usw3" || legacy.DevTunnel.TunnelName != "" {
		t.Fatalf("legacy id inherit changed: %+v", legacy.DevTunnel)
	}
	pending, err := b.Create(devCustomPayload("pending", "", model.DevTunnelConfig{TunnelName: "Web-App", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	if pending.DevTunnel.TunnelName != "web-app" || pending.DevTunnel.TunnelID != "" {
		t.Fatalf("legacy pending name lost: %+v", pending.DevTunnel)
	}
}

func TestDevTunnelPendingNameConflict(t *testing.T) {
	b := metadataCacheBiz(t, t.TempDir())
	first, err := b.Create(devCustomPayload("first", "custom", model.DevTunnelConfig{TunnelName: "web-app", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create(devCustomPayload("second", "custom", model.DevTunnelConfig{TunnelName: "Web-App", Access: "private"})); err == nil {
		t.Fatal("duplicate pending name accepted")
	}
	if _, err := b.Create(devCustomPayload("third", "bind", model.DevTunnelConfig{TunnelID: "web-app", Access: "private"})); err == nil {
		t.Fatal("binding a pending name was accepted")
	}
	if _, err := b.Create(devCustomPayload("fourth", "bind", model.DevTunnelConfig{TunnelID: "web-app.usw3", Access: "private"})); err == nil {
		t.Fatal("binding the cluster form of a pending name was accepted")
	}
	// Switching the first record back to auto frees the name again.
	if _, err := b.Update(first.ID, devCustomPayload("first", "auto", baseDevConfig())); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create(devCustomPayload("second", "custom", model.DevTunnelConfig{TunnelName: "web-app", Access: "private"})); err != nil {
		t.Fatal(err)
	}
}

func TestDevTunnelConfigRoundTripPreservesPendingName(t *testing.T) {
	dir := t.TempDir()
	b := metadataCacheBiz(t, dir)
	created, err := b.Create(devCustomPayload("first", "custom", model.DevTunnelConfig{TunnelName: "web-dev-app", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	var found *model.Tunnel
	for i := range cfg.Tunnels {
		if cfg.Tunnels[i].ID == created.ID {
			found = &cfg.Tunnels[i]
		}
	}
	if found == nil {
		t.Fatal("tunnel missing after reload")
	}
	if found.DevTunnel == nil || found.DevTunnel.TunnelName != "web-dev-app" || found.DevTunnel.TunnelID != "" {
		t.Fatalf("round trip lost the pending name: %+v", found.DevTunnel)
	}
}

func TestDevTunnelFirstStartUsesCustomName(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.txt")
	b := metadataCacheBiz(t, dir)
	b.dev.Binary = createDevScript(t, logPath, "")
	b.newDevServiceHost = func(string, []devtunnel.ServiceTarget) devServiceHost {
		return &fakeDevServiceHost{targets: []devtunnel.ServiceTarget{{Host: "127.0.0.1", Port: 8080, LocalPort: 8080}}, done: make(chan struct{}), events: make(chan forward.RuntimeEvent, 16)}
	}
	created, err := b.Create(devCustomPayload("first", "custom", model.DevTunnelConfig{TunnelName: "web-dev-app", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	task, ok := b.tunnelByID(created.ID)
	if !ok {
		t.Fatal("tunnel missing")
	}
	b.mu.Lock()
	b.starting[task.ID] = &task
	b.mu.Unlock()
	if _, err := b.newStartedDevRuntime(&task); err != nil {
		t.Fatal(err)
	}
	args := readArgsLog(t, logPath)
	if !strings.Contains(args, "create web-dev-app --expiration 30d -j") {
		t.Fatalf("custom name was not passed to the cli: %s", args)
	}
	cfg, err := b.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	stored := cfg.Tunnels[0]
	if stored.DevTunnel.TunnelID != "web-dev-app.usw3" || stored.DevTunnel.TunnelName != "" {
		t.Fatalf("persisted = %+v", stored.DevTunnel)
	}

	// Restarting reuses the real id and never creates again.
	os.WriteFile(logPath, nil, 0600)
	b.mu.Lock()
	b.starting[task.ID] = &task
	delete(b.inflight, task.ID)
	delete(b.devSessions, strings.ToLower("web-dev-app.usw3"))
	b.mu.Unlock()
	if _, err := b.newStartedDevRuntime(&task); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readArgsLog(t, logPath), "create ") {
		t.Fatalf("restart created again: %s", readArgsLog(t, logPath))
	}
	cfg, err = b.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnels[0].DevTunnel.TunnelID != "web-dev-app.usw3" {
		t.Fatalf("restart changed the id: %+v", cfg.Tunnels[0].DevTunnel)
	}
}

func TestDevTunnelFirstStartConflictKeepsPendingName(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.txt")
	b := metadataCacheBiz(t, dir)
	b.dev.Binary = createDevScript(t, logPath, "conflict")
	b.newDevServiceHost = func(string, []devtunnel.ServiceTarget) devServiceHost {
		return &fakeDevServiceHost{done: make(chan struct{}), events: make(chan forward.RuntimeEvent, 16)}
	}
	created, err := b.Create(devCustomPayload("first", "custom", model.DevTunnelConfig{TunnelName: "taken", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	task, ok := b.tunnelByID(created.ID)
	if !ok {
		t.Fatal("tunnel missing")
	}
	b.mu.Lock()
	b.starting[task.ID] = &task
	b.mu.Unlock()
	_, startErr := b.newStartedDevRuntime(&task)
	if !errors.Is(startErr, devtunnel.ErrTunnelNameConflict) {
		t.Fatalf("err = %v, want ErrTunnelNameConflict", startErr)
	}
	cfg, err := b.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnels[0].DevTunnel.TunnelName != "taken" || cfg.Tunnels[0].DevTunnel.TunnelID != "" {
		t.Fatalf("conflict lost the pending name: %+v", cfg.Tunnels[0].DevTunnel)
	}
	if strings.Contains(readArgsLog(t, logPath), "delete") {
		t.Fatalf("conflict deleted a remote resource: %s", readArgsLog(t, logPath))
	}
}

func readArgsLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

func createDevScript(t *testing.T, logPath, mode string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "devtunnel")
	onCreate := `echo '{"tunnelId":"web-dev-app","clusterId":"usw3"}'; exit 0`
	if mode == "conflict" {
		onCreate = `echo 'Tunnel "taken" already exists.' >&2; exit 1`
	}
	script := `#!/bin/sh
echo "$@" >> "` + logPath + `"
if [ "$1" = "--version" ]; then
  echo "Tunnel CLI version: 1.2.3"
  exit 0
fi
if [ "$1" = "user" ]; then
  echo '{"status":"Signed in"}'
  exit 0
fi
if [ "$1" = "create" ]; then
  ` + onCreate + `
fi
if [ "$1" = "show" ]; then
  echo '{"tunnelId":"web-dev-app","clusterId":"usw3","ports":[{"portNumber":8080,"protocol":"http"}],"allowAnonymous":false}'
  exit 0
fi
if [ "$1" = "delete" ]; then
  exit 0
fi
echo "unexpected $*" >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return binary
}
