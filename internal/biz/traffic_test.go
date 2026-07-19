package biz

import (
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

func TestTrafficSnapshot_Empty(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	b := NewTunnelBiz(storage)
	up, down := b.TrafficSnapshot()
	if up != 0 || down != 0 {
		t.Fatalf("TrafficSnapshot() = (%d, %d), want (0, 0)", up, down)
	}
}

func TestTrafficSnapshot_AggregatesRuns(t *testing.T) {
	dir := t.TempDir()
	storage, err := conf.NewStorage(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	b := NewTunnelBiz(storage)
	f1 := forward.NewLocalForward(model.Tunnel{ID: 1, Name: "one"}, nil)
	f2 := forward.NewLocalForward(model.Tunnel{ID: 2, Name: "two"}, nil)

	pumpBridgeTraffic(t, f1, []byte("abc"), []byte("de"))
	pumpBridgeTraffic(t, f2, []byte("fg"), []byte("hij"))

	b.mu.Lock()
	b.runs[1] = f1
	b.runs[2] = f2
	b.mu.Unlock()

	up, down := b.TrafficSnapshot()
	if up != 5 {
		t.Fatalf("aggregated upload = %d, want 5", up)
	}
	if down != 5 {
		t.Fatalf("aggregated download = %d, want 5", down)
	}
}

func pumpBridgeTraffic(t *testing.T, f *forward.LocalForward, upload, download []byte) {
	t.Helper()

	localServer, localClient := net.Pipe()
	remoteServer, remoteClient := net.Pipe()

	done := make(chan struct{})
	go func() {
		f.Bridge(localServer, remoteServer)
		close(done)
	}()

	uploadDone := make(chan struct{})
	go func() {
		defer close(uploadDone)
		buf := make([]byte, len(upload))
		if _, err := io.ReadFull(remoteClient, buf); err != nil {
			t.Errorf("read upload: %v", err)
		}
	}()
	if _, err := localClient.Write(upload); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	<-uploadDone

	downloadDone := make(chan struct{})
	go func() {
		defer close(downloadDone)
		buf := make([]byte, len(download))
		if _, err := io.ReadFull(localClient, buf); err != nil {
			t.Errorf("read download: %v", err)
		}
	}()
	if _, err := remoteClient.Write(download); err != nil {
		t.Fatalf("write download: %v", err)
	}
	<-downloadDone

	_ = localClient.Close()
	_ = remoteClient.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not finish in time")
	}
}
