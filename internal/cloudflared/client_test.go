package cloudflared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseVersionAndLogin(t *testing.T) {
	if got := ParseVersion("cloudflared version 2026.1.0 (built 2026-01-02)\n"); got != "2026.1.0" {
		t.Fatalf("version = %q", got)
	}
	phase, url := ClassifyLoginLine("Please open the following URL and log in: https://dash.cloudflare.com/argotunnel?aud=1")
	if phase != "browser" || !strings.HasPrefix(url, "https://dash.cloudflare.com/") {
		t.Fatalf("login line = %s %s", phase, url)
	}
	if phase, _ := ClassifyLoginLine("You have successfully logged in."); phase != "verifying" {
		t.Fatalf("success phase = %s", phase)
	}
}

func TestResolvePrefersManagedBinary(t *testing.T) {
	home := t.TempDir()
	previous := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = previous })
	managed := filepath.Join(home, ".loris-tunnel", "bin", binaryName())
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path, source, err := NewClient("").resolve()
	if err != nil || path != managed || source != "managed" {
		t.Fatalf("resolve = %s %s %v", path, source, err)
	}
	missing := NewClient(filepath.Join(t.TempDir(), "missing"))
	if _, _, err := missing.resolve(); err == nil {
		t.Fatal("missing binary was accepted")
	}
}

func TestInspectAccountStates(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "cloudflared")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "cloudflared version 2026.2.0"; exit 0; fi
if [ "$2" = "list" ]; then echo 'null'; exit 0; fi
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	client := NewClient(binary)
	client.ConfigHome = home
	status, err := client.Inspect(t.Context())
	if err != nil || status.AccountState != "signed_out" || status.Version != "2026.2.0" {
		t.Fatalf("signed out status = %+v %v", status, err)
	}
	if err := os.WriteFile(filepath.Join(home, "cert.pem"), []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = client.Inspect(t.Context())
	if err != nil || status.AccountState != "signed_in" {
		t.Fatalf("signed in status = %+v %v", status, err)
	}
}

func TestLoginRestoresCertWhenAuthorizationTimesOut(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "cloudflared")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "cloudflared version 2026.2.0"; exit 0; fi
if [ "$2" = "list" ]; then echo 'null'; exit 0; fi
echo "A browser window should have opened at the following URL:"
echo "https://dash.cloudflare.com/argotunnel?aud=1"
echo "Waiting for login..."
echo "Failed to fetch resource"
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	client := NewClient(binary)
	client.ConfigHome = home
	cert := filepath.Join(home, "cert.pem")
	if err := os.WriteFile(cert, []byte("old-cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var sawURL bool
	if err := client.StartLogin(func(_, url string) {
		if strings.Contains(url, "dash.cloudflare.com") {
			sawURL = true
		}
	}, func() { close(done) }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("login did not finish")
	}
	body, err := os.ReadFile(cert)
	if err != nil || string(body) != "old-cert" {
		t.Fatalf("cert = %q %v", body, err)
	}
	if !sawURL {
		t.Fatal("login did not report the browser URL")
	}
	status, err := client.Inspect(t.Context())
	if err != nil || status.AccountState != "signed_in" || status.LoginError != "cloudflare_login_incomplete_kept" || status.LoginInProgress {
		t.Fatalf("status = %+v %v", status, err)
	}
}

func TestCredentialFiles(t *testing.T) {
	id := "03e12345-6789-4abc-8def-0123456789ab"
	client := NewClient("")
	client.ConfigHome = t.TempDir()
	if client.CredentialExists(id) {
		t.Fatal("missing credential reported ready")
	}
	if client.CredentialExists("not-a-uuid") {
		t.Fatal("wrong id reported ready")
	}
	path, err := client.CredentialPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !client.CredentialExists(strings.ToUpper(id)) {
		t.Fatal("credential was not found")
	}
	previous := userHomeDir
	userHomeDir = func() (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { userHomeDir = previous })
	if _, err := NewClient("").CredentialPath(id); err == nil {
		t.Fatal("missing home was accepted")
	}
}
