package biz

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

func TestLegacyStoppedTunnelStillAutoStarts(t *testing.T) {
	b := createAutoStartTunnels(t, 1)
	data, err := os.ReadFile(b.storage.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "manually_stopped") {
			lines = append(lines, line)
		}
	}
	if err := os.WriteFile(b.storage.Path(), []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	b = NewTunnelBiz(b.storage)
	if err := startAutoStartAndWait(b, 0); err != nil {
		t.Fatal(err)
	}
	if got := countAttemptedAutoStart(t, b); got != 1 {
		t.Fatalf("legacy stopped tunnel was skipped: %d", got)
	}
}

func TestManualStopSurvivesRestartAndManualStartFailureResumesAutoStart(t *testing.T) {
	b := createAutoStartTunnels(t, 1)
	// An idle runtime exercises the stop path without external SSH dependencies.
	b.runs[1] = forward.NewLocalForward(model.Tunnel{ID: 1}, nil)
	stopped, err := b.Toggle(1, func() int { t.Fatal("stop must skip license check"); return 0 })
	if err != nil || stopped.Status != "stopped" || !stopped.ManuallyStopped {
		t.Fatalf("manual stop: %+v, %v", stopped, err)
	}
	b = NewTunnelBiz(b.storage)
	if err := startAutoStartAndWait(b, 0); err != nil {
		t.Fatal(err)
	}
	if got := countAttemptedAutoStart(t, b); got != 0 {
		t.Fatalf("manually stopped tunnel was auto-started: %d", got)
	}
	// The fixture's SSH endpoint is unavailable. The accepted start must still
	// persist resumed intent before reporting the connection error.
	updated, err := b.Toggle(1, func() int { return 0 })
	if err != nil || updated.Status != "busy" || updated.ManuallyStopped {
		t.Fatalf("failed manual start should resume intent before connecting: %+v, %v", updated, err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		items, listErr := b.List()
		if listErr != nil {
			t.Fatal(listErr)
		}
		if items[0].Status == "error" && !items[0].ManuallyStopped {
			updated = items[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("failed manual start should resume intent: %+v", items)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if updated.Status != "error" || updated.ManuallyStopped {
		t.Fatalf("failed manual start should resume intent: %+v", updated)
	}
	if _, err := b.updateStatus(1, "stopped", ""); err != nil {
		t.Fatal(err)
	}
	b = NewTunnelBiz(b.storage)
	if err := startAutoStartAndWait(b, 0); err != nil {
		t.Fatal(err)
	}
	items, err := b.List()
	if err != nil || items[0].ManuallyStopped {
		t.Fatalf("resumed intent was not persisted: %+v, %v", items, err)
	}
	if got := countAttemptedAutoStart(t, b); got != 1 {
		t.Fatalf("resumed tunnel was not attempted: %d", got)
	}
}

func TestAutoStartFiltersManualStopsBeforeLimit(t *testing.T) {
	b := createAutoStartTunnels(t, 5)
	_, err := b.storage.Update(func(c *conf.Config) error {
		c.Tunnels[0].ManuallyStopped = true
		c.Tunnels[0].Status = "running"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := startAutoStartAndWait(b, 3); err != nil {
		t.Fatal(err)
	}
	c, err := b.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Tunnels[0].ManuallyStopped || c.Tunnels[0].Status != "stopped" {
		t.Fatal("state cleanup lost manual stop")
	}
	for _, tunnel := range c.Tunnels[1:4] {
		if tunnel.Status != "error" || tunnel.ManuallyStopped {
			t.Fatalf("eligible tunnel was not attempted: %+v", tunnel)
		}
	}
	if c.Tunnels[4].Status != "stopped" || c.Tunnels[4].ManuallyStopped {
		t.Fatal("limit skip changed intent")
	}
}

func TestRejectedStartPreservesManualStop(t *testing.T) {
	for _, reason := range []string{"limit", "storage"} {
		t.Run(reason, func(t *testing.T) {
			b := createAutoStartTunnels(t, 1)
			_, err := b.storage.Update(func(c *conf.Config) error { c.Tunnels[0].ManuallyStopped = true; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if reason == "limit" {
				b.runs[99] = nil
			} else {
				original, readErr := os.ReadFile(b.storage.Path())
				if readErr != nil {
					t.Fatal(readErr)
				}
				if err = os.Rename(b.storage.Path(), b.storage.Path()+".saved"); err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(b.storage.Path(), 0700); err != nil {
					t.Fatal(err)
				}
				defer os.WriteFile(b.storage.Path(), original, 0600)
			}
			_, err = b.Toggle(1, func() int { return 1 })
			if err == nil {
				t.Fatal("expected rejection")
			}
			if reason == "limit" && !errors.Is(err, ErrFreePlanRunningLimit) {
				t.Fatal(err)
			}
			if reason == "storage" {
				os.Remove(b.storage.Path())
				if err = os.Rename(b.storage.Path()+".saved", b.storage.Path()); err != nil {
					t.Fatal(err)
				}
			}
			c, loadErr := b.storage.Load()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if !c.Tunnels[0].ManuallyStopped || c.Tunnels[0].Status != "stopped" {
				t.Fatal("rejected start changed state or intent")
			}
		})
	}
}

func TestInternalLifecyclePreservesManualStopIntent(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		b := createAutoStartTunnels(t, 1)
		_, err := b.storage.Update(func(c *conf.Config) error { c.Tunnels[0].ManuallyStopped = stopped; return nil })
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"error", "running"} {
			updated, err := b.updateStatus(1, status, "")
			if err != nil || updated.ManuallyStopped != stopped {
				t.Fatalf("lifecycle changed intent: %+v, %v", updated, err)
			}
		}
		b.runs[1] = forward.NewLocalForward(model.Tunnel{ID: 1}, nil)
		b.Shutdown()
		c, err := b.storage.Load()
		if err != nil {
			t.Fatal(err)
		}
		if c.Tunnels[0].Status != "stopped" || c.Tunnels[0].ManuallyStopped != stopped {
			t.Fatal("shutdown changed intent")
		}
	}
}

func TestEditingPreservesManualStopIntent(t *testing.T) {
	b := createAutoStartTunnels(t, 1)
	_, err := b.storage.Update(func(c *conf.Config) error { c.Tunnels[0].ManuallyStopped = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, autoStart := range []bool{false, true} {
		updated, err := b.Update(1, model.TunnelPayload{
			Name: "edited", Mode: "local", JumperIDs: []int{1},
			LocalHost: "127.0.0.1", LocalPort: 18000, RemoteHost: "10.0.0.1", RemotePort: 22,
			AutoStart: autoStart, Status: "stopped",
		})
		if err != nil || !updated.ManuallyStopped {
			t.Fatalf("edit lost manual stop: %+v, %v", updated, err)
		}
	}
}
