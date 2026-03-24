package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadImportCandidates(t *testing.T) {
	rootDir := t.TempDir()
	sshDir := filepath.Join(rootDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir ssh dir failed: %v", err)
	}

	includePath := filepath.Join(sshDir, "work.conf")
	if err := os.WriteFile(includePath, []byte(`
Host app-prod
  HostName 10.10.0.25
  User deploy
  Port 2200
  IdentityFile ~/.ssh/id_work
`), 0o644); err != nil {
		t.Fatalf("write include failed: %v", err)
	}

	configPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(configPath, []byte(`
Host *
  User root
  ServerAliveInterval 15
  ConnectTimeout 8

Include work.conf

Host bastion
  HostName bastion.example.com
  User ops
  Port 2222
  IdentityAgent ~/.ssh/agent.sock
  StrictHostKeyChecking no

Host internal
  HostName internal.example.com
  ProxyJump bastion

Host *.corp
  User ignored
`), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	result, err := LoadImportCandidates(configPath)
	if err != nil {
		t.Fatalf("LoadImportCandidates failed: %v", err)
	}

	if len(result.Candidates) != 3 {
		t.Fatalf("Candidates len = %d, want 3", len(result.Candidates))
	}

	first := result.Candidates[0]
	if first.Alias != "app-prod" {
		t.Fatalf("first alias = %q, want app-prod", first.Alias)
	}
	if first.User != "deploy" {
		t.Fatalf("app-prod user = %q, want deploy", first.User)
	}
	if first.Port != 2200 {
		t.Fatalf("app-prod port = %d, want 2200", first.Port)
	}
	if first.KeyPath != "~/.ssh/id_work" {
		t.Fatalf("app-prod keyPath = %q, want ~/.ssh/id_work", first.KeyPath)
	}
	if first.KeepAliveIntervalMs != 15000 {
		t.Fatalf("app-prod keepAlive = %d, want 15000", first.KeepAliveIntervalMs)
	}
	if first.TimeoutMs != 8000 {
		t.Fatalf("app-prod timeout = %d, want 8000", first.TimeoutMs)
	}

	second := result.Candidates[1]
	if second.Alias != "bastion" {
		t.Fatalf("second alias = %q, want bastion", second.Alias)
	}
	if !second.BypassHostVerification {
		t.Fatalf("bastion bypassHostVerification = false, want true")
	}
	if second.AuthType != "ssh_agent" {
		t.Fatalf("bastion authType = %q, want ssh_agent", second.AuthType)
	}

	third := result.Candidates[2]
	if third.Alias != "internal" {
		t.Fatalf("third alias = %q, want internal", third.Alias)
	}
	if third.ProxyJump != "bastion" {
		t.Fatalf("internal proxyJump = %q, want bastion", third.ProxyJump)
	}
	if !contains(third.Warnings, "proxy_jump") {
		t.Fatalf("internal warnings = %v, want proxy_jump", third.Warnings)
	}
}

func TestLoadImportCandidatesMissingFile(t *testing.T) {
	_, err := LoadImportCandidates(filepath.Join(t.TempDir(), "missing-config"))
	if err == nil {
		t.Fatal("expected missing file error")
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
