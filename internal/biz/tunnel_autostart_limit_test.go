package biz

import (
	"errors"
	"path/filepath"
	"testing"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/model"
)

func createAutoStartTunnels(t *testing.T, n int) *TunnelBiz {
	t.Helper()
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	jumperBiz := NewJumperBiz(storage)
	jumper, err := jumperBiz.Create(model.JumperPayload{
		Name:     "jump",
		Host:     "127.0.0.1",
		Port:     1,
		User:     "root",
		AuthType: "ssh_agent",
	})
	if err != nil {
		t.Fatalf("create jumper: %v", err)
	}

	tunnelBiz := NewTunnelBiz(storage)
	for i := 0; i < n; i++ {
		_, err := tunnelBiz.Create(model.TunnelPayload{
			Name:       "t" + string(rune('a'+i)),
			Mode:       "local",
			JumperIDs:  []int{jumper.ID},
			LocalHost:  "127.0.0.1",
			LocalPort:  18000 + i,
			RemoteHost: "10.0.0.1",
			RemotePort: 22,
			AutoStart:  true,
			Status:     "stopped",
		})
		if err != nil {
			t.Fatalf("create tunnel %d: %v", i, err)
		}
	}
	return tunnelBiz
}

func countAttemptedAutoStart(t *testing.T, tunnelBiz *TunnelBiz) int {
	t.Helper()
	cfg, err := tunnelBiz.storage.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	attempted := 0
	for _, tunnel := range cfg.Tunnels {
		if tunnel.AutoStart && tunnel.Status != "stopped" {
			attempted++
		}
	}
	return attempted
}

func TestStartAutoStartRespectsFreePlanLimit(t *testing.T) {
	tunnelBiz := createAutoStartTunnels(t, 5)
	_ = tunnelBiz.StartAutoStart(FreePlanRunningLimit)

	attempted := countAttemptedAutoStart(t, tunnelBiz)
	if attempted != FreePlanRunningLimit {
		t.Fatalf("expected exactly %d auto-start attempts, got %d", FreePlanRunningLimit, attempted)
	}
}

func TestStartAutoStartUnlimitedWhenMaxRunningZero(t *testing.T) {
	tunnelBiz := createAutoStartTunnels(t, 5)
	_ = tunnelBiz.StartAutoStart(0)

	attempted := countAttemptedAutoStart(t, tunnelBiz)
	if attempted != 5 {
		t.Fatalf("expected all 5 auto-start attempts when unlimited, got %d", attempted)
	}
}

func TestToggleEnforcesFreePlanLimit(t *testing.T) {
	tunnelBiz := createAutoStartTunnels(t, 4)
	tunnelBiz.mu.Lock()
	tunnelBiz.runs[9001] = nil
	tunnelBiz.runs[9002] = nil
	tunnelBiz.runs[9003] = nil
	tunnelBiz.mu.Unlock()

	cfg, err := tunnelBiz.storage.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	targetID := cfg.Tunnels[0].ID

	_, err = tunnelBiz.Toggle(targetID, FreePlanRunningLimit)
	if !errors.Is(err, ErrFreePlanRunningLimit) {
		t.Fatalf("expected ErrFreePlanRunningLimit, got %v", err)
	}
}
