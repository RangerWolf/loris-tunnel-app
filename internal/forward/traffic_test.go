package forward

import (
	"io"
	"net"
	"testing"
	"time"

	"loris-tunnel/internal/model"
)

func TestBridgeCountsUploadAndDownload(t *testing.T) {
	f := NewLocalForward(model.Tunnel{ID: 1, Name: "test"}, nil)

	localServer, localClient := net.Pipe()
	remoteServer, remoteClient := net.Pipe()

	done := make(chan struct{})
	go func() {
		f.Bridge(localServer, remoteServer)
		close(done)
	}()

	uploadPayload := []byte("upload-bytes-payload")
	downloadPayload := []byte("download-bytes-payload")

	uploadDone := make(chan struct{})
	go func() {
		defer close(uploadDone)
		buf := make([]byte, len(uploadPayload))
		if _, err := io.ReadFull(remoteClient, buf); err != nil {
			t.Errorf("read upload payload: %v", err)
			return
		}
		if string(buf) != string(uploadPayload) {
			t.Errorf("upload payload = %q, want %q", buf, uploadPayload)
		}
	}()

	if _, err := localClient.Write(uploadPayload); err != nil {
		t.Fatalf("write upload payload: %v", err)
	}
	<-uploadDone

	downloadDone := make(chan struct{})
	go func() {
		defer close(downloadDone)
		buf := make([]byte, len(downloadPayload))
		if _, err := io.ReadFull(localClient, buf); err != nil {
			t.Errorf("read download payload: %v", err)
			return
		}
		if string(buf) != string(downloadPayload) {
			t.Errorf("download payload = %q, want %q", buf, downloadPayload)
		}
	}()

	if _, err := remoteClient.Write(downloadPayload); err != nil {
		t.Fatalf("write download payload: %v", err)
	}
	<-downloadDone

	_ = localClient.Close()
	_ = remoteClient.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not finish in time")
	}

	up, down := f.Traffic()
	if up != uint64(len(uploadPayload)) {
		t.Fatalf("upload bytes = %d, want %d", up, len(uploadPayload))
	}
	if down != uint64(len(downloadPayload)) {
		t.Fatalf("download bytes = %d, want %d", down, len(downloadPayload))
	}
}
