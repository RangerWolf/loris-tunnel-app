package devtunnel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeTunnelNameValid(t *testing.T) {
	cases := map[string]string{
		"web-dev-app":                       "web-dev-app",
		"api3000":                           "api3000",
		" Web-Dev-App ":                     "web-dev-app",
		"ABC":                               "abc",
		"a-b":                               "a-b",
		strings.Repeat("a", 49):             strings.Repeat("a", 49),
		"0-start":                           "0-start",
		"my--tunnel":                        "my--tunnel",
		"end9":                              "end9",
		strings.Repeat("x", 48) + "9":       strings.Repeat("x", 48) + "9",
	}
	for input, want := range cases {
		got, err := NormalizeTunnelName(input)
		if err != nil {
			t.Fatalf("NormalizeTunnelName(%q) = %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeTunnelName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeTunnelNameInvalid(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"a",
		"ab",
		strings.Repeat("a", 50),
		"my_project",
		"demo test",
		"-frontend-",
		"-frontend",
		"frontend-",
		"v1.api.server",
		"demo.test",
		"内部名称",
		"café",
		"has space",
	}
	for _, input := range cases {
		if got, err := NormalizeTunnelName(input); err == nil {
			t.Fatalf("NormalizeTunnelName(%q) = %q, want error", input, got)
		}
	}
}

func TestTunnelNameConflictDetection(t *testing.T) {
	positive := []string{
		"Tunnel 'web-dev-app' already exists.",
		"{\"error\":{\"code\":\"TunnelNameAlreadyExists\",\"message\":\"already exists\"}}",
		"create failed: 409 conflict",
	}
	negative := []string{
		"port conflicts with an existing port",
		"login token expired",
		"forbidden: not authorized",
		"",
	}
	for _, text := range positive {
		if !tunnelNameConflict(text) {
			t.Fatalf("tunnelNameConflict(%q) = false", text)
		}
	}
	for _, text := range negative {
		if tunnelNameConflict(text) {
			t.Fatalf("tunnelNameConflict(%q) = true", text)
		}
	}
}

func TestCreatePersistentCustomName(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args.txt")
	client := NewClient(regionScript(t, logPath, "match"))
	id, err := client.CreatePersistent(context.Background(), false, "30d", "auto", " Web-Dev-App ")
	if err != nil {
		t.Fatal(err)
	}
	if id != "abc123.usw3" {
		t.Fatal(id)
	}
	args := readRegionLog(t, logPath)
	if !strings.Contains(args, "create web-dev-app --expiration 30d -j") {
		t.Fatalf("custom name was not passed: %s", args)
	}

	os.WriteFile(logPath, nil, 0600)
	if _, err := client.CreatePersistent(context.Background(), false, "30d", "auto", "bad_name"); err == nil {
		t.Fatal("invalid name was accepted")
	}
	if readRegionLog(t, logPath) != "" {
		t.Fatalf("invalid name still ran the cli: %s", readRegionLog(t, logPath))
	}
}

func TestCreatePersistentConflict(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args.txt")
	client := NewClient(conflictScript(t, logPath))
	_, err := client.CreatePersistent(context.Background(), false, "30d", "auto", "taken")
	if !errors.Is(err, ErrTunnelNameConflict) {
		t.Fatalf("err = %v, want ErrTunnelNameConflict", err)
	}
	args := readRegionLog(t, logPath)
	if strings.Contains(args, "delete") {
		t.Fatalf("conflict must not delete anything: %s", args)
	}
}

func TestCreatePersistentErrorBeforeID(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args.txt")
	client := NewClient(failedCreateScript(t, logPath))
	_, err := client.CreatePersistent(context.Background(), false, "30d", "auto", "")
	if err == nil || errors.Is(err, ErrTunnelNameConflict) {
		t.Fatalf("err = %v, want a plain create failure", err)
	}
	args := readRegionLog(t, logPath)
	if strings.Contains(args, "delete") {
		t.Fatalf("failed create must not delete anything: %s", args)
	}
}

func conflictScript(t *testing.T, logPath string) string {
	t.Helper()
	return createScript(t, logPath, "echo 'Tunnel \"taken\" already exists.' >&2; exit 1")
}

func failedCreateScript(t *testing.T, logPath string) string {
	t.Helper()
	// The CLI failed but its output still contains a parseable tunnel id.
	// The caller must treat this as a failure and never delete by name.
	return createScript(t, logPath, "echo '{\"tunnelId\":\"abc123\",\"clusterId\":\"usw3\"}'; exit 1")
}

func createScript(t *testing.T, logPath, onCreate string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "devtunnel")
	script := `#!/bin/sh
echo "$@" >> "` + logPath + `"
if [ "$1" = "user" ]; then
  echo "Token: test-token"
  exit 0
fi
if [ "$1" = "create" ]; then
  ` + onCreate + `
fi
echo "unexpected $*" >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return binary
}
