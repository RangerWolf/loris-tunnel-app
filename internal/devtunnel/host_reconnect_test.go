package devtunnel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loris-tunnel/internal/forward"
)

func TestHostFailureClassification(t *testing.T) {
	for _, tc := range []struct{ text, kind string }{
		{"SSH disconnected: TooManyConnections conflict", "host_conflict"},
		{"Too many connections", "host_conflict"},
		{"HTTP 429", "transient"},
		{"relay network timeout", "transient"},
		{"authentication handshake transport EOF", "transient"},
		{"expired relay session", "transient"},
		{"Connect via browser: https://conflict-8080.example\nReady to accept connections for tunnel: invalid-tunnel.usw3\nrelay network lost", "transient"},
		{"Login token expired", "auth"},
		{"permission denied", "fatal"},
		{"tunnel not found", "fatal"},
		{"configuration conflict", "fatal"},
		{"contain CLI process failed", "fatal"},
	} {
		if got := hostFailureKind(errors.New(tc.text)); got != tc.kind {
			t.Errorf("%q: %s, want %s", tc.text, got, tc.kind)
		}
	}
	for _, line := range []string{"HTTP 429 too many connections", "GET /too many connections", "relay reconnecting"} {
		if hostDisconnectLine(line) {
			t.Errorf("must not terminate for %q", line)
		}
	}
}

func TestReconnectDelay(t *testing.T) {
	for i, want := range []int{1, 2, 4, 8, 15, 30, 30, 30} {
		if got := reconnectDelay(time.Second, i+1); got != time.Duration(want)*time.Second {
			t.Fatalf("attempt %d: %s", i+1, got)
		}
	}
	if reconnectDelay(time.Second, 100000) != 30*time.Second {
		t.Fatal("long outage must retain capped retries")
	}
	h := NewClient("").NewHost("abc.usw3", "localhost", 3000)
	for i := 0; i < 100; i++ {
		d := h.jitter(time.Second)
		if d < 800*time.Millisecond || d > 1200*time.Millisecond {
			t.Fatal(d)
		}
	}
}

func TestHostAutomaticRecovery(t *testing.T) {
	for _, mode := range []string{"recovery-exit", "recovery-live", "recovery-transient", "recovery-probe-error"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := lifecycleClient(t, mode)
			h := c.NewHost("abc.usw3", "127.0.0.1", 3000)
			h.retryDelay = time.Millisecond
			h.jitter = func(d time.Duration) time.Duration { return d }
			if err := h.Start(); err != nil {
				t.Fatal(err)
			}
			defer h.Stop()
			sawDisconnect := false
			timeout := time.After(5 * time.Second)
			for {
				select {
				case e := <-h.Events():
					if e.Type == forward.RuntimeEventDisconnected {
						sawDisconnect = true
						if e.Reconnect == nil || e.Reconnect.Attempt < 1 {
							t.Fatal("missing diagnostics")
						}
						if mode == "recovery-probe-error" && len(e.Reconnect.HostIDs) != 0 {
							t.Fatal("failed query invented identity")
						}
						if mode != "recovery-transient" && mode != "recovery-probe-error" && (e.Reconnect.Kind != "host_conflict" || len(e.Reconnect.HostIDs) != 1 || e.Reconnect.HostIDs[0] != "observed-peer") {
							t.Fatalf("diagnostic=%+v", e.Reconnect)
						}
					}
					if e.Type == forward.RuntimeEventReconnected {
						if !sawDisconnect || h.URL() == "" || h.Err() != nil {
							t.Fatal("invalid recovery")
						}
						return
					}
				case <-h.Done():
					t.Fatalf("unexpected terminal failure: %v", h.Err())
				case <-timeout:
					t.Fatal("recovery timed out")
				}
			}
		})
	}
}

func TestHostFlappingRetainsBackoffAndStableResets(t *testing.T) {
	c, _ := lifecycleClient(t, "recovery-flap")
	h := c.NewHost("abc.usw3", "127.0.0.1", 3000)
	h.retryDelay = time.Millisecond
	h.jitter = func(d time.Duration) time.Duration { return d }
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	for _, want := range []int{1, 2, 3} {
		select {
		case e := <-h.Events():
			// Recovery events occur between disconnections.
			if e.Type == forward.RuntimeEventReconnected {
				e = <-h.Events()
			}
			if e.Reconnect == nil || e.Reconnect.Attempt != want {
				t.Fatalf("event=%+v, want attempt %d", e, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing event")
		}
	}
}

func TestHostAuthenticationFailureEndsRecovery(t *testing.T) {
	c, _ := lifecycleClient(t, "recovery-auth")
	h := c.NewHost("abc.usw3", "127.0.0.1", 3000)
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	select {
	case <-h.Done():
		if !strings.Contains(h.Err().Error(), "login expired") {
			t.Fatal(h.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("auth failure retried")
	}
}

func TestObservedHostIDs(t *testing.T) {
	got := parseObservedHostIDs(`{"endpoints":[{"hostId":"peer"},{"hostId":"peer"},{"hostId":"https://secret?token=x"},{"hostId":"other"}]}`)
	if strings.Join(got, ",") != "peer,other" {
		t.Fatal(got)
	}
	for _, out := range []string{`{}`, `not json`, `{"endpoints":null}`} {
		if len(parseObservedHostIDs(out)) != 0 {
			t.Fatal(out)
		}
	}
	c, _ := lifecycleClient(t, "recovery-live")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if len(c.observedHostIDs(ctx, "abc.usw3")) != 0 {
		t.Fatal("canceled diagnostics returned IDs")
	}
}

func TestHostStableConnectionResetsFailures(t *testing.T) {
	c, _ := lifecycleClient(t, "recovery-flap")
	h := c.NewHost("abc.usw3", "127.0.0.1", 3000)
	h.retryDelay = time.Millisecond
	// A zero injected threshold makes stability deterministic under CPU load.
	h.stableWindow = 0
	h.jitter = func(d time.Duration) time.Duration { return d }
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	disconnects := 0
	deadline := time.After(5 * time.Second)
	for disconnects < 3 {
		select {
		case e := <-h.Events():
			if e.Type == forward.RuntimeEventDisconnected {
				disconnects++
				if e.Reconnect.Attempt != 1 {
					t.Fatalf("stable connection retained failures: %+v", e.Reconnect)
				}
			}
		case <-deadline:
			t.Fatal("missing stable reconnect events")
		}
	}
}

func TestHostDiagnosticSanitization(t *testing.T) {
	text := hostDiagnosticText("relay disconnected: https://host/path?token=top-secret access_token=private\n{\"token\":\"json-secret\"}\nToken: opaque-secret")
	for _, secret := range []string{"top-secret", "private", "json-secret", "opaque-secret", "https://"} {
		if strings.Contains(text, secret) {
			t.Fatalf("leaked %s: %s", secret, text)
		}
	}
	if !strings.Contains(text, "relay disconnected") {
		t.Fatal("lost context")
	}
}

func TestHostStopDuringDiagnosticQuery(t *testing.T) {
	c, state := lifecycleClient(t, "recovery-probe-hang")
	h := c.NewHost("abc.usw3", "127.0.0.1", 3000)
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	awaitCondition(t, func() bool { _, err := os.Stat(state + ".probe"); return err == nil })
	started := time.Now()
	if err := h.Stop(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("diagnostic blocked cancellation")
	}
	data, _ := os.ReadFile(state)
	if string(data) != "1" {
		t.Fatalf("stop started another CLI: %s", data)
	}
}

func TestHostFallbackRetriesNetworkButPreservesPortSafety(t *testing.T) {
	for _, unsafe := range []bool{false, true} {
		t.Run(fmt.Sprint(unsafe), func(t *testing.T) {
			dir := t.TempDir()
			script := `#!/bin/sh
if [ "$1" = "show" ]; then
 n=0; [ ! -f "COUNTER.show" ] || n=$(cat "COUNTER.show")
 n=$((n+1)); echo "$n" > "COUNTER.show"
 if [ "$n" = 2 ]; then echo 'network timeout' >&2; exit 1; fi
 if [ "$n" -ge 3 ] && [ "UNSAFE" = true ]; then
  echo '{"tunnelId":"abc","clusterId":"usw3","ports":[{"portNumber":9090,"protocol":"http"}]}'
 else
  echo '{"tunnelId":"abc","clusterId":"usw3","ports":[{"portNumber":8080,"protocol":"http"}]}'
 fi
 exit 0
fi
if [ "$1" = "host" ]; then
 if [ "$#" -gt 2 ]; then echo 'Batch update of ports is not supported'; exit 1; fi
 n=0; [ ! -f "COUNTER.host" ] || n=$(cat "COUNTER.host")
 n=$((n+1)); echo "$n" > "COUNTER.host"
 echo 'Connect via browser: https://abc-8080.usw3.devtunnels.ms'
 echo 'Ready to accept connections for tunnel: abc.usw3'
 if [ "$n" = 1 ]; then sleep 0.1; echo 'relay network lost' >&2; exit 1; fi
 while :; do sleep 1; done
fi
exit 2
`
			counter := filepath.Join(dir, "calls")
			script = strings.ReplaceAll(strings.ReplaceAll(script, "COUNTER", counter), "UNSAFE", fmt.Sprint(unsafe))
			binary := filepath.Join(dir, "devtunnel")
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			h := NewClient(binary).NewServiceHost("abc.usw3", []ServiceTarget{{Host: "127.0.0.1", Port: 8080, LocalPort: 8080}})
			h.retryDelay = time.Millisecond
			if err := h.Start(); err != nil {
				t.Fatal(err)
			}
			defer h.Stop()
			timeout := time.After(5 * time.Second)
			for {
				select {
				case <-h.Done():
					if !unsafe || h.Err() == nil || !strings.Contains(h.Err().Error(), "cannot safely host") {
						t.Fatalf("unexpected terminal error: %v", h.Err())
					}
					return
				case e := <-h.Events():
					if e.Type == forward.RuntimeEventReconnected {
						if unsafe {
							t.Fatal("unsafe ports reconnected")
						}
						return
					}
				case <-timeout:
					t.Fatal("fallback recovery timed out")
				}
			}
		})
	}
}
