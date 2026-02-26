package forward

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"loris-tunnel/internal/model"

	"golang.org/x/crypto/ssh"
)

func TestReconnectPolicyConstants(t *testing.T) {
	if initReconnectWait != 500*time.Millisecond {
		t.Fatalf("initReconnectWait = %v, want %v", initReconnectWait, 500*time.Millisecond)
	}
	if maxReconnectWait != time.Minute {
		t.Fatalf("maxReconnectWait = %v, want %v", maxReconnectWait, time.Minute)
	}
	if reconnectTimeout != 15*time.Minute {
		t.Fatalf("reconnectTimeout = %v, want %v", reconnectTimeout, 15*time.Minute)
	}
}

func TestNextReconnectWaitSequence(t *testing.T) {
	wait := initReconnectWait
	seq := []time.Duration{wait}
	for i := 0; i < 8; i++ {
		wait = nextReconnectWait(wait)
		seq = append(seq, wait)
	}

	want := []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		1 * time.Minute,
		1 * time.Minute,
	}

	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("seq[%d] = %v, want %v", i, seq[i], want[i])
		}
	}
}

func TestKeepAliveIntervalDefault(t *testing.T) {
	j := model.Jumper{KeepAliveIntervalMs: 0}
	if got := keepAliveInterval(j); got != 0 {
		t.Fatalf("keepAliveInterval default = %v, want %v", got, 0*time.Second)
	}
}

func TestKeepAliveIntervalFromJumper(t *testing.T) {
	j := model.Jumper{KeepAliveIntervalMs: 7000}
	if got := keepAliveInterval(j); got != 7*time.Second {
		t.Fatalf("keepAliveInterval = %v, want %v", got, 7*time.Second)
	}
}

func TestKeepAliveRequestTimeoutBoundaries(t *testing.T) {
	cases := []struct {
		interval time.Duration
		want     time.Duration
	}{
		{interval: 0, want: 3 * time.Second},
		{interval: 2 * time.Second, want: 2 * time.Second},
		{interval: 5 * time.Second, want: 2500 * time.Millisecond},
		{interval: 12 * time.Second, want: 5 * time.Second},
	}

	for _, tc := range cases {
		if got := keepAliveRequestTimeout(tc.interval); got != tc.want {
			t.Fatalf("keepAliveRequestTimeout(%v) = %v, want %v", tc.interval, got, tc.want)
		}
	}
}

func TestEnsureSignerSupportsLegacyRSA(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key failed: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("build signer failed: %v", err)
	}

	compatible := ensureSignerSupportsLegacyRSA(signer)
	multiSigner, ok := compatible.(ssh.MultiAlgorithmSigner)
	if !ok {
		t.Fatalf("compatible signer should implement MultiAlgorithmSigner")
	}

	algorithms := multiSigner.Algorithms()
	if !containsAlgorithm(algorithms, ssh.KeyAlgoRSA) {
		t.Fatalf("algorithms %v should include %s", algorithms, ssh.KeyAlgoRSA)
	}
	if !containsAlgorithm(algorithms, ssh.KeyAlgoRSASHA256) {
		t.Fatalf("algorithms %v should include %s", algorithms, ssh.KeyAlgoRSASHA256)
	}
	if !containsAlgorithm(algorithms, ssh.KeyAlgoRSASHA512) {
		t.Fatalf("algorithms %v should include %s", algorithms, ssh.KeyAlgoRSASHA512)
	}
}

func TestEnsureSignerSupportsLegacyRSA_NonRSAUnchanged(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key failed: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("build signer failed: %v", err)
	}

	compatible := ensureSignerSupportsLegacyRSA(signer)
	if compatible.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Fatalf("key type changed unexpectedly: %s", compatible.PublicKey().Type())
	}
}

func TestGetSSHAgentSigners_NoSock(t *testing.T) {
	resetSSHAgent()
	t.Setenv("LORIS_TUNNEL_SSH_AUTH_SOCK", "")
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", t.TempDir())

	_, err := getSSHAgentSigners(model.Jumper{})
	if err == nil {
		t.Fatalf("expected error when SSH_AUTH_SOCK is missing")
	}
	if !strings.Contains(err.Error(), "no usable identities") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentSocketCandidates_OrderAndNormalization(t *testing.T) {
	resetSSHAgent()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LORIS_TUNNEL_SSH_AUTH_SOCK", "~/custom-agent.sock")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/another-agent.sock")

	candidates := agentSocketCandidates("")
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %v", candidates)
	}
	wantFirst := filepath.Join(home, "custom-agent.sock")
	if candidates[0] != wantFirst {
		t.Fatalf("first candidate = %s, want %s", candidates[0], wantFirst)
	}
	if candidates[1] != "/tmp/another-agent.sock" {
		t.Fatalf("second candidate = %s, want /tmp/another-agent.sock", candidates[1])
	}
	if runtime.GOOS != "windows" {
		defaultSock := filepath.Join(home, ".ssh", "ssh-agent.sock")
		if !containsString(candidates, defaultSock) {
			t.Fatalf("expected default candidate %s in %v", defaultSock, candidates)
		}
	}
}

func TestAgentSocketCandidates_Dedup(t *testing.T) {
	resetSSHAgent()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LORIS_TUNNEL_SSH_AUTH_SOCK", "/tmp/shared-agent.sock")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/shared-agent.sock")

	candidates := agentSocketCandidates("")
	if countString(candidates, "/tmp/shared-agent.sock") != 1 {
		t.Fatalf("expected deduped shared socket in %v", candidates)
	}
}

func TestAgentSocketCandidates_PreferJumperSocket(t *testing.T) {
	resetSSHAgent()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LORIS_TUNNEL_SSH_AUTH_SOCK", "/tmp/loris-agent.sock")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/system-agent.sock")

	candidates := agentSocketCandidates("~/user-selected.sock")
	wantFirst := filepath.Join(home, "user-selected.sock")
	if len(candidates) == 0 || candidates[0] != wantFirst {
		t.Fatalf("first candidate = %v, want %s", candidates, wantFirst)
	}
}

func containsAlgorithm(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func containsString(list []string, target string) bool {
	return countString(list, target) > 0
}

func countString(list []string, target string) int {
	count := 0
	for _, item := range list {
		if item == target {
			count++
		}
	}
	return count
}
