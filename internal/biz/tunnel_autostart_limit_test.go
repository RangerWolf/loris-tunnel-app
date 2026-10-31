package biz

import (
	"errors"
	"path/filepath"
	"testing"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/forward"
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
	_ = startAutoStartAndWait(tunnelBiz, FreePlanRunningLimit)

	attempted := countAttemptedAutoStart(t, tunnelBiz)
	if attempted != FreePlanRunningLimit {
		t.Fatalf("expected exactly %d auto-start attempts, got %d", FreePlanRunningLimit, attempted)
	}
}

func TestStartAutoStartAllowsUnlimitedWhenMaxRunningZero(t *testing.T) {
	tunnelBiz := createAutoStartTunnels(t, 5)
	_ = startAutoStartAndWait(tunnelBiz, 0)

	attempted := countAttemptedAutoStart(t, tunnelBiz)
	if attempted != 5 {
		t.Fatalf("expected all 5 Pro auto-start attempts, got %d", attempted)
	}
}

func TestStartAutoStartClearsSkippedRuntimeStates(t *testing.T) {
	b := createAutoStartTunnels(t, 6)
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		for i := range cfg.Tunnels {
			cfg.Tunnels[i].Status = "running"
		}
		cfg.Tunnels[4].Status = "busy"
		cfg.Tunnels[5].AutoStart = false
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := startAutoStartAndWait(b, FreePlanRunningLimit); err != nil {
		t.Fatal(err)
	}
	cfg, err := b.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tunnel := range cfg.Tunnels[FreePlanRunningLimit:] {
		if tunnel.Status != "stopped" {
			t.Fatalf("skipped tunnel %s has stale status %s", tunnel.Name, tunnel.Status)
		}
	}
}

func TestListDoesNotReportPersistedRunningWithoutRuntime(t *testing.T) {
	b := createAutoStartTunnels(t, 1)
	if _, err := b.updateStatus(1, "running", ""); err != nil {
		t.Fatal(err)
	}
	items, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Status != "stopped" || items[0].LatencyMs != 0 {
		t.Fatalf("no runtime should report stopped without latency: %+v", items[0])
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

	calls := 0
	_, err = tunnelBiz.Toggle(targetID, func() int {
		calls++
		return FreePlanRunningLimit
	})
	if !errors.Is(err, ErrFreePlanRunningLimit) {
		t.Fatalf("expected ErrFreePlanRunningLimit, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one license check when starting, got %d", calls)
	}
}

func TestToggleStopSkipsLicenseCheck(t *testing.T) {
	tunnelBiz := createAutoStartTunnels(t, 1)
	cfg, err := tunnelBiz.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	id := cfg.Tunnels[0].ID
	if _, err := tunnelBiz.updateStatus(id, "running", ""); err != nil {
		t.Fatal(err)
	}
	tunnelBiz.runs[id] = forward.NewLocalForward(model.Tunnel{ID: id}, nil)
	updated, err := tunnelBiz.Toggle(id, func() int {
		t.Fatal("stopping must not query the license service")
		return FreePlanRunningLimit
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "stopped" {
		t.Fatalf("expected stopped, got %s", updated.Status)
	}
}

func startAutoStartAndWait(b *TunnelBiz, limit int) error {
	err := b.StartAutoStart(limit)
	b.startWG.Wait()
	return err
}

func TestToggleProCanStartBeyondThree(t *testing.T) {
	b := createAutoStartTunnels(t, 1)
	cfg, err := b.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	for id := 100; id < 103; id++ {
		b.runs[id] = forward.NewLocalForward(model.Tunnel{ID: id}, nil)
	}
	result, err := b.Toggle(cfg.Tunnels[0].ID, func() int { return 0 })
	b.startWG.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "busy" {
		t.Fatalf("expected accepted async start, got %s", result.Status)
	}
}
