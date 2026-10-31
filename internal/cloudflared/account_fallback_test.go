package cloudflared

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountCredentialFallback(t *testing.T) {
	const namedTunnel = `[{"id":"03e12345-6789-4abc-8def-0123456789ab","name":"authentication required cert.pem"}]`
	cases := []struct {
		name       string
		localA     string
		outputA    string
		exitA      int
		rejectB    bool
		missingB   bool
		wantState  string
		wantCalls  string
		wantTunnel bool
	}{
		{name: "missing PEM", localA: "missing", wantState: ResourceReady, wantCalls: "b"},
		{name: "empty PEM", localA: "empty", wantState: ResourceReady, wantCalls: "b"},
		{name: "corrupt PEM", localA: "corrupt", wantState: ResourceReady, wantCalls: "b"},
		{name: "zero exit certificate error", outputA: "Error locating origin cert: cannot find a valid certificate", wantState: ResourceReady, wantCalls: "ab"},
		{name: "zero exit certificate decode error", outputA: "Error decoding origin cert: invalid PEM", wantState: ResourceReady, wantCalls: "ab"},
		{name: "zero exit JSON auth error", outputA: `{"level":"error","message":"authentication is required"}`, wantState: ResourceReady, wantCalls: "ab"},
		{name: "zero exit timestamped auth error", outputA: "2026-10-25T00:00:00Z ERR Unauthorized", wantState: ResourceReady, wantCalls: "ab"},
		{name: "zero exit permission error", outputA: "Error: forbidden", wantState: ResourceReady, wantCalls: "ab"},
		{name: "nonzero permission error", outputA: "permission denied", exitA: 1, wantState: ResourceReady, wantCalls: "ab"},
		{name: "all local credentials invalid", localA: "corrupt", missingB: true, wantState: ResourceAuth, wantCalls: ""},
		{name: "all remote authorizations rejected", outputA: "Error: authentication is required", rejectB: true, wantState: ResourceAuth, wantCalls: "ab"},
		{name: "network errors do not replay commands", outputA: "i/o timeout", exitA: 1, wantState: ResourceNetwork, wantCalls: "a"},
		{name: "resource errors do not replay commands", outputA: "tunnel not found", exitA: 1, wantState: ResourceNotFound, wantCalls: "a"},
		{name: "successful resource names are not diagnostics", outputA: namedTunnel, wantState: ResourceReady, wantCalls: "a", wantTunnel: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(filepath.Join(t.TempDir(), "cloudflared"))
			client.ConfigHome = t.TempDir()
			dir, _ := client.authorizationDir()
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			store := authorizationStore{AccountID: "account"}
			var paths []string
			for _, key := range []string{"a", "b"} {
				writeOriginCert(t, client.ConfigHome, key, "zone-"+key)
				raw, err := os.ReadFile(filepath.Join(client.ConfigHome, "cert.pem"))
				if err != nil {
					t.Fatal(err)
				}
				file := fmt.Sprintf("%x.pem", sha256.Sum256(raw))
				path := filepath.Join(dir, file)
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				store.Entries = append(store.Entries, authorizationEntry{Zone: Zone{ID: "zone-" + key}, File: file})
				paths = append(paths, path)
			}
			if err := client.saveAuthorizationsLocked(store); err != nil {
				t.Fatal(err)
			}
			switch tc.localA {
			case "missing":
				if err := os.Remove(paths[0]); err != nil {
					t.Fatal(err)
				}
			case "empty", "corrupt":
				body := ""
				if tc.localA == "corrupt" {
					body = "broken PEM"
				}
				if err := os.WriteFile(paths[0], []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.missingB {
				if err := os.Remove(paths[1]); err != nil {
					t.Fatal(err)
				}
			}
			outputA := tc.outputA
			if outputA == "" {
				outputA = "[]"
			}
			outputB := "[]"
			if tc.rejectB {
				outputB = "Error: authentication is required"
			}
			log := filepath.Join(t.TempDir(), "attempts")
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
			script := fmt.Sprintf(`#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--origincert" ]; then cert="$2"; shift; fi
  shift
done
if [ "$cert" = %s ]; then
  printf a >> %s
  printf '%%s\n' %s
  exit %d
fi
printf b >> %s
printf '%%s\n' %s
`, quote(paths[0]), quote(log), quote(outputA), tc.exitA, quote(log), quote(outputB))
			if err := os.WriteFile(client.Binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			result := client.ListResources(t.Context())
			if result.State != tc.wantState {
				t.Fatalf("state = %s, detail = %s; want %s", result.State, result.Detail, tc.wantState)
			}
			attempts, err := os.ReadFile(log)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(attempts) != tc.wantCalls {
				t.Fatalf("credential attempts = %q; want %q", attempts, tc.wantCalls)
			}
			if tc.wantTunnel && (len(result.Items) != 1 || result.Items[0].Name != "authentication required cert.pem") {
				t.Fatal("valid tunnel data was mistaken for an authorization failure", result)
			}
		})
	}
}

func TestLegacyAccountZeroExitAuthFailure(t *testing.T) {
	client := NewClient(filepath.Join(t.TempDir(), "cloudflared"))
	client.ConfigHome = t.TempDir()
	// Preserve the legacy path where the official CLI interprets the credential.
	if err := os.WriteFile(filepath.Join(client.ConfigHome, "cert.pem"), []byte("legacy certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(client.Binary, []byte("#!/bin/sh\necho 'Error locating origin cert: invalid certificate' >&2\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if result := client.ListResources(t.Context()); result.State != ResourceAuth {
		t.Fatalf("zero exit auth error was treated as %s", result.State)
	}
}

func TestAccountCLIAuthFailureIgnoresSuccessfulDiagnostics(t *testing.T) {
	for _, output := range []string{
		`{"level":"info","message":"origin certificate loaded"}`,
		`[{"name":"Error locating origin cert: authentication required"}]`,
		`[{"name":"2026 ERR authentication required"}]`,
		"2026-10-25T00:00:00Z INF origin certificate loaded",
		"Error: tunnel not found",
	} {
		if accountCLIAuthFailure(output) {
			t.Errorf("not an authorization error: %s", output)
		}
	}
}
