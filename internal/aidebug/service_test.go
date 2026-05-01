package aidebug

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loris-tunnel/internal/model"
)

func TestMatchRulesPublicKeyOfferedButRejected(t *testing.T) {
	rawError := "ssh handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"
	debugOutput := `
debug1: Offering public key: wenjun-yang.pem explicit
debug1: Authentications that can continue: publickey
debug1: No more authentication methods to try.
Permission denied (publickey).
`

	rules := matchRules(rawError, debugOutput)
	if !containsRule(rules, "Public key was offered but rejected by remote server") {
		t.Fatalf("expected remote public key rejection rule, got %#v", rules)
	}
	if containsRule(rules, "Key file is invalid or unreadable") {
		t.Fatalf("did not expect local key file rule, got %#v", rules)
	}
}

func TestResolveKeyPathFallsBackToSSHDir(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	keyPath := filepath.Join(sshDir, "wenjun-yang.pem")
	if err := os.WriteFile(keyPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("HOME", home)

	resolved, err := resolveKeyPath("wenjun-yang.pem")
	if err != nil {
		t.Fatalf("resolveKeyPath() error = %v", err)
	}
	if resolved != keyPath {
		t.Fatalf("resolveKeyPath() = %q, want %q", resolved, keyPath)
	}
}

func TestBuildSSHConfigResolvesIdentityFile(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	keyPath := filepath.Join(sshDir, "wenjun-yang.pem")
	if err := os.WriteFile(keyPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("HOME", home)

	cfgPath, cleanup, checks := buildSSHConfig([]model.Jumper{{
		Host:     "example.com",
		User:     "root",
		AuthType: "ssh_key",
		KeyPath:  "wenjun-yang.pem",
	}})
	t.Cleanup(func() {
		if cleanup != nil {
			cleanup()
		}
	})
	if len(checks) == 0 || checks[0].Status != "ok" {
		t.Fatalf("buildSSHConfig() checks = %#v, want ok", checks)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "IdentityFile "+keyPath) {
		t.Fatalf("config does not contain resolved identity file path:\n%s", string(data))
	}
}

func TestNormalizeLLMConfidenceAcceptsNumbers(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "high decimal", value: 0.9, want: "high"},
		{name: "medium decimal", value: 0.5, want: "medium"},
		{name: "low decimal", value: 0.2, want: "low"},
		{name: "high percent", value: 85.0, want: "high"},
		{name: "string number", value: "0.75", want: "high"},
		{name: "localized high", value: "高", want: "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLLMConfidence(tt.value); got != tt.want {
				t.Fatalf("normalizeLLMConfidence(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestCallLLMUsesBackendDiagnoseEndpoint(t *testing.T) {
	transport := &captureTransport{
		statusCode: http.StatusOK,
		body: `{
			"reason":"Remote user mismatch.",
			"summary":"The SSH server rejected the login.",
			"steps":["Check the remote user","Install the public key"],
			"confidence":"high",
			"locale":"en",
			"usedFallback":false
		}`,
	}

	service := NewService("https://backend.example/api/v1", "machine-1")
	service.httpClient.Transport = transport
	result, err := service.callLLM(
		context.Background(),
		"en",
		"permission denied",
		[]string{"rule"},
		[]model.AIDebugCheck{{Name: "ssh", Status: "ok", Detail: "checked"}},
		sshClient{Path: "/usr/bin/ssh", Version: "OpenSSH"},
		"debug output",
		DiagnosticInput{TargetType: "tunnel_test"},
	)
	if err != nil {
		t.Fatalf("callLLM() error = %v", err)
	}
	if transport.path != "/api/v1/ai-debug/diagnose" {
		t.Fatalf("request path = %q, want /api/v1/ai-debug/diagnose", transport.path)
	}
	if transport.authorization != "" {
		t.Fatalf("did not expect client Authorization header, got %q", transport.authorization)
	}
	if result.Reason != "Remote user mismatch." || result.Confidence != "high" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCallLLMReturnsQuotaError(t *testing.T) {
	service := NewService("https://backend.example/api/v1", "machine-1")
	service.httpClient.Transport = &captureTransport{
		statusCode: http.StatusTooManyRequests,
		body:       `{"detail":"AI debug daily quota exceeded for free plan (10/day)"}`,
	}
	_, err := service.callLLM(
		context.Background(),
		"en",
		"permission denied",
		nil,
		nil,
		sshClient{},
		"",
		DiagnosticInput{TargetType: "tunnel_test"},
	)
	if err == nil {
		t.Fatal("callLLM() expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 429") || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("quota error = %q", err.Error())
	}
}

func TestDiagnoseReturnsErrorWhenBackendUnavailable(t *testing.T) {
	service := NewService("https://backend.example/api/v1", "machine-1")
	service.httpClient.Transport = &captureTransport{
		statusCode: http.StatusInternalServerError,
		body:       `{"detail":"temporary backend failure"}`,
	}

	result, err := service.Diagnose(context.Background(), DiagnosticInput{
		TargetType: "jumper_test",
		RawError:   "permission denied",
		UILocale:   "en",
		JumperChain: []model.Jumper{{
			Name:     "demo",
			Host:     "127.0.0.1",
			Port:     1,
			User:     "root",
			AuthType: "ssh_key",
		}},
	})
	if err == nil {
		t.Fatalf("Diagnose() expected backend error, got result %#v", result)
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("Diagnose() error = %q, want HTTP 500", err.Error())
	}
}

func containsRule(rules []string, want string) bool {
	for _, rule := range rules {
		if rule == want {
			return true
		}
	}
	return false
}

type captureTransport struct {
	statusCode    int
	body          string
	path          string
	authorization string
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.path = req.URL.Path
	t.authorization = req.Header.Get("Authorization")
	return &http.Response{
		StatusCode: t.statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Request:    req,
	}, nil
}
