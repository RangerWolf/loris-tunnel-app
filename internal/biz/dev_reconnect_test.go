package biz

import (
	"fmt"
	"testing"
	"time"

	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

func TestDevReconnectStateAndStop(t *testing.T) {
	b := metadataCacheBiz(t, t.TempDir())
	created, err := b.Create(devCustomPayload("service", "bind", model.DevTunnelConfig{TunnelID: "abc.usw3", Access: "private"}))
	if err != nil {
		t.Fatal(err)
	}
	r := serviceRuntime(b, created.ID, 8080, 8080)
	b.mu.Lock()
	b.runs[created.ID] = r
	b.runProvider[created.ID] = model.ProviderDevTunnel
	b.mu.Unlock()
	if _, err = b.updateStatus(created.ID, "running", ""); err != nil {
		t.Fatal(err)
	}
	watched := make(chan struct{})
	go func() { b.watchRuntime(created.ID, r); close(watched) }()
	defer func() { _ = b.stopRuntime(created.ID); <-watched }()
	awaitStatus := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			cfg, e := b.storage.Load()
			if e != nil {
				t.Fatal(e)
			}
			for _, item := range cfg.Tunnels {
				if item.ID == created.ID && item.Status == want {
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("missing status %s", want)
	}
	for _, attempt := range []int{1, 3} {
		r.emit(forward.RuntimeEvent{Type: forward.RuntimeEventDisconnected, Err: fmt.Errorf("host disconnected"), Reconnect: &model.ReconnectInfo{Attempt: attempt, Kind: "host_conflict"}})
		awaitStatus("reconnecting")
		if b.RunningCount() != 1 {
			t.Fatal("retry lost reserved slot")
		}
		r.emit(forward.RuntimeEvent{Type: forward.RuntimeEventReconnected})
		awaitStatus("running")
		cfg, _ := b.storage.Load()
		if cfg.Tunnels[0].LastError != "" {
			t.Fatal("recovery retained error")
		}
	}
	if err = b.stopRuntime(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.updateStatus(created.ID, "stopped", ""); err != nil {
		t.Fatal(err)
	}
	<-watched
	// Completed stop releases ownership and the reconnect watcher.
	awaitStatus("stopped")
}
