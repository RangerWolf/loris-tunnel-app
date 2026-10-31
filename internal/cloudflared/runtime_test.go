package cloudflared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostReadyAndStop(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "cloudflared")
	script := `#!/bin/sh
echo "Registered tunnel connection connIndex=0"
while true; do sleep 0.2; done
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	host := NewClient(binary).NewHost("03e12345-6789-4abc-8def-0123456789ab", filepath.Join(t.TempDir(), "run.yml"), "dev.example.com")
	host.readyTimeout = 2 * time.Second
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	if host.PublicURL() != "https://dev.example.com" {
		t.Fatalf("url = %s", host.PublicURL())
	}
	if err := host.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestHostQUICFallback(t *testing.T) {
	for _, mode := range []string{"ready", "failed", "auth", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			binary, args := filepath.Join(dir, "cloudflared"), filepath.Join(dir, "args")
			script := `#!/bin/sh
echo "$*" >> "` + args + `"
case "$*" in
 *"--protocol auto"*)
 `
			if mode == "auth" {
				script += "echo 'authentication is required'; exit 1\n"
			} else {
				script += "echo 'Failed to dial a quic connection: timeout: no recent network activity'\nwhile true; do sleep 0.1; done\n"
			}
			script += ";;\n *)\n"
			switch mode {
			case "ready":
				script += "echo 'Registered tunnel connection connIndex=0'\nwhile true; do sleep 0.1; done\n"
			case "cancel":
				script += "while true; do sleep 0.1; done\n"
			default:
				script += "echo 'dial tcp edge:7844: connection refused'; exit 1\n"
			}
			script += ";;\nesac\n"
			if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			host := NewClient(binary).NewHost("03e12345-6789-4abc-8def-0123456789ab", filepath.Join(dir, "run.yml"), "dev.example.com")
			host.readyTimeout = 2 * time.Second
			defer host.Stop()
			result := make(chan error, 1)
			go func() { result <- host.Start() }()
			if mode == "cancel" {
				deadline := time.Now().Add(3 * time.Second)
				for {
					data, _ := os.ReadFile(args)
					if strings.Contains(string(data), "--protocol http2") {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("fallback did not start")
					}
					time.Sleep(10 * time.Millisecond)
				}
				if err := host.Stop(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-result:
				if mode == "ready" && err != nil {
					t.Fatal(err)
				}
				if mode != "ready" && err == nil {
					t.Fatal("failure reported success")
				}
				if mode == "failed" && !strings.Contains(err.Error(), "connection refused") {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Start did not finish")
			}
			data, _ := os.ReadFile(args)
			count := strings.Count(string(data), "--protocol http2")
			if mode == "auth" && count != 0 {
				t.Fatal("auth failure triggered fallback")
			}
			if mode != "auth" && count != 1 {
				t.Fatalf("expected exactly one fallback: %s", data)
			}
		})
	}
}
