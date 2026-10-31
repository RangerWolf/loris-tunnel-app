package cloudflared

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestInstallRejectsNonBinaryAndAcceptsScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell cloudflared stand-in")
	}
	home := t.TempDir()
	previous := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = previous })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"cloudflared version 2026.4.1\"; fi\n"))
	}))
	defer server.Close()
	client := NewClient("")
	reqURL = server.URL
	t.Cleanup(func() { reqURL = "" })
	if err := client.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	target := managedBinaryPath()
	out, err := client.run(context.Background(), target, "--version")
	if err != nil || ParseVersion(out) != "2026.4.1" {
		t.Fatalf("installed version = %q %v", out, err)
	}
}

func TestInstallCancel(t *testing.T) {
	home := t.TempDir()
	previous := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = previous })
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	reqURL = server.URL
	t.Cleanup(func() { reqURL = "" })
	client := NewClient("")
	done := make(chan error, 1)
	go func() { done <- client.Install(context.Background()) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not start")
	}
	client.CancelInstall()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled install succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("install did not cancel")
	}
}
