package biz

import (
	"errors"
	"net"
	"testing"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/model"
)

func TestAutoStartDoesNotBlockListAndReservesLimit(t *testing.T) {
	// Hold the SSH handshake open to verify behavior during actual network work.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		<-release
	}()
	b := createAutoStartTunnels(t, 2)
	_, err = b.storage.Update(func(c *conf.Config) error {
		c.Jumpers[0].AuthType = "password"
		c.Jumpers[0].Password = "test"
		c.Jumpers[0].BypassHostVerification = true
		c.Jumpers[0].Port = ln.Addr().(*net.TCPAddr).Port
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { started <- b.StartAutoStart(1) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("auto-start waited for SSH handshake")
	}
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("SSH start was not attempted")
	}
	listed := make(chan []model.Tunnel, 1)
	go func() { items, _ := b.List(); listed <- items }()
	select {
	case items := <-listed:
		if len(items) != 2 || items[0].Status != "busy" || items[1].Status != "stopped" {
			t.Fatalf("unexpected in-flight state: %+v", items)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("list blocked on SSH handshake")
	}
	if b.RunningCount() != 1 {
		t.Fatal("pending start did not reserve a slot")
	}
	if err := b.StartAutoStart(1); err != nil {
		t.Fatal(err)
	}
	if b.RunningCount() != 1 {
		t.Fatal("repeat auto-start reserved duplicate slots")
	}
	_, err = b.Toggle(2, func() int { return 1 })
	if !errors.Is(err, ErrFreePlanRunningLimit) {
		t.Fatalf("pending start bypassed limit: %v", err)
	}
	// Deletion invalidates the in-flight task; its eventual failure must not
	// overwrite a replacement tunnel with the same numeric ID.
	if err := b.Delete(1); err != nil {
		t.Fatal(err)
	}
	_, err = b.storage.Update(func(c *conf.Config) error {
		c.Tunnels = append(c.Tunnels, model.Tunnel{ID: 1, Name: "replacement", Status: "stopped"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	b.startWG.Wait()
	c, err := b.storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Tunnels[1].Status != "stopped" || c.Tunnels[1].LastError != "" {
		t.Fatal("late task changed replacement state")
	}
	if b.RunningCount() != 0 {
		t.Fatal("canceled start leaked a slot")
	}
}

func TestToggleStartsStaleRunningWithoutRuntime(t *testing.T) {
	b := createAutoStartTunnels(t, 1)
	if _, err := b.updateStatus(1, "running", ""); err != nil {
		t.Fatal(err)
	}
	checks := 0
	updated, err := b.Toggle(1, func() int { checks++; return 1 })
	if err != nil {
		t.Fatal(err)
	}
	if checks != 1 || updated.Status != "busy" || updated.ManuallyStopped {
		t.Fatalf("start click was treated as stop because of stale status: %+v", updated)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		items, listErr := b.List()
		if listErr != nil {
			t.Fatal(listErr)
		}
		if items[0].Status == "error" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("stale start did not report the connection error")
}
