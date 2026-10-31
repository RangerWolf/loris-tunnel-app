package biz

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"loris-tunnel/internal/forward"
	"loris-tunnel/internal/model"
)

func TestMeasurePublicLatency(t *testing.T) {
	var otherRequests atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherRequests.Add(1) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("probe sent unexpected method or credentials")
		}
		switch r.URL.Path {
		case "/auth":
			w.WriteHeader(401)
		case "/forbidden":
			w.WriteHeader(403)
		case "/missing":
			w.WriteHeader(404)
		case "/rate-limit":
			w.WriteHeader(429)
		case "/error":
			w.WriteHeader(503)
		case "/redirect":
			http.Redirect(w, r, "/ok", 302)
		case "/loop":
			http.Redirect(w, r, "/loop", 302)
		case "/external":
			http.Redirect(w, r, other.URL+"/login?token=secret", 302)
		case "/slow":
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
		case "/body":
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		path, state string
		code        int
	}{
		{"/ok", "ok", 200}, {"/auth", "ok", 401}, {"/forbidden", "ok", 403},
		{"/missing", "ok", 404}, {"/rate-limit", "ok", 429},
		{"/error", "ok", 503}, {"/redirect", "ok", 200}, {"/external", "ok", 302},
		{"/slow", "timeout", 0}, {"/loop", "failed", 0}, {"/body", "ok", 200},
	} {
		t.Run(tc.path, func(t *testing.T) {
			result := measurePublicLatency(context.Background(), server.Client().Transport, server.URL+tc.path, 100*time.Millisecond)
			if result.state != tc.state || result.code != tc.code {
				t.Fatalf("result=%+v", result)
			}
			if result.state == "ok" && result.ms < 1 {
				t.Fatal("missing elapsed time")
			}
			if result.state != "ok" && result.ms != 0 {
				t.Fatal("failure retained latency")
			}
			if result.checked == "" {
				t.Fatal("missing timestamp")
			}
			if strings.Contains(result.detail, "secret") {
				t.Fatal("redirect token exposed")
			}
		})
	}
	if otherRequests.Load() != 0 {
		t.Fatal("followed cross-origin redirect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := measurePublicLatency(ctx, server.Client().Transport, server.URL, time.Second); result.state != "failed" {
		t.Fatalf("canceled probe=%+v", result)
	}
}

type probeTestRuntime struct {
	mu   sync.Mutex
	url  string
	done chan struct{}
	once sync.Once
}

func (r *probeTestRuntime) PublicURL() string                   { r.mu.Lock(); defer r.mu.Unlock(); return r.url }
func (r *probeTestRuntime) setURL(s string)                     { r.mu.Lock(); r.url = s; r.mu.Unlock() }
func (r *probeTestRuntime) Stop() error                         { r.once.Do(func() { close(r.done) }); return nil }
func (r *probeTestRuntime) Done() <-chan struct{}               { return r.done }
func (r *probeTestRuntime) Events() <-chan forward.RuntimeEvent { return nil }
func (r *probeTestRuntime) Err() error                          { return nil }

func awaitProbe(t *testing.T, b *TunnelBiz, run tunnelRuntime, want string) model.Tunnel {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		item := model.Tunnel{ID: 1}
		b.attachPublicLatency(&item, run)
		if item.LatencyState == want {
			return item
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("probe never reached %s", want)
	return model.Tunnel{}
}

func TestPublicLatencyLifecycle(t *testing.T) {
	entered := make(chan struct{}, 1)
	canceled := make(chan struct{}, 1)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
		canceled <- struct{}{}
	}))
	defer slow.Close()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer ok.Close()
	b := NewTunnelBiz(nil)
	run := &probeTestRuntime{url: slow.URL, done: make(chan struct{})}
	b.runs[1] = run
	tunnel := model.Tunnel{ID: 1, Provider: model.ProviderCloudflare}
	b.startPublicLatency(tunnel, run)
	defer func() { b.stopRuntime(1); b.probeWG.Wait() }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("probe never started")
	}
	run.setURL(ok.URL)
	// Changed addresses cannot display the previous address's measurement.
	item := model.Tunnel{ID: 1}
	b.attachPublicLatency(&item, run)
	if item.LatencyState != "measuring" || item.LatencyMs != 0 {
		t.Fatalf("stale URL result=%+v", item)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("URL change did not cancel old probe")
	}
	result := awaitProbe(t, b, run, "ok")
	if result.LatencyHTTPStatus != 204 {
		t.Fatal(result)
	}
	if err := b.stopRuntime(1); err != nil {
		t.Fatal(err)
	}
	replacement := &probeTestRuntime{url: ok.URL, done: make(chan struct{})}
	b.mu.Lock()
	b.runs[1] = replacement
	b.mu.Unlock()
	b.startPublicLatency(tunnel, replacement)
	awaitProbe(t, b, replacement, "ok")
	stale := model.Tunnel{ID: 1}
	b.attachPublicLatency(&stale, run)
	if stale.LatencyMs != 0 {
		t.Fatal("old runtime observed replacement result")
	}
}

func TestPrivateDevTunnelDoesNotProbe(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer server.Close()
	b := NewTunnelBiz(nil)
	run := &probeTestRuntime{url: server.URL, done: make(chan struct{})}
	b.runs[1] = run
	b.startPublicLatency(model.Tunnel{ID: 1, Provider: model.ProviderDevTunnel, DevTunnel: &model.DevTunnelConfig{Access: "private"}}, run)
	awaitProbe(t, b, run, "auth_required")
	b.stopRuntime(1)
	b.probeWG.Wait()
	if requests.Load() != 0 {
		t.Fatal("private tunnel contacted login endpoint")
	}
}

func TestPreferredProbeURL(t *testing.T) {
	if got := preferredProbeURL("http://host:8080, https://host:8080, https://host/"); got != "https://host/" {
		t.Fatal(got)
	}
	if got := latencyTarget("frp.example.com:13000"); got != "frp.example.com:13000" {
		t.Fatal(got)
	}
}

func TestTCPLatencyMeasuresConnectTime(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	result := measureTCPLatency(context.Background(), ln.Addr().String(), time.Second)
	if result.state != "ok" || result.ms <= 0 {
		t.Fatal(result)
	}
}

func TestStoppingPublicLatencyCancelsPendingRequest(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(canceled) }))
	defer server.Close()
	b := NewTunnelBiz(nil)
	run := &probeTestRuntime{url: server.URL, done: make(chan struct{})}
	b.runs[1] = run
	b.startPublicLatency(model.Tunnel{ID: 1, Provider: model.ProviderCloudflare}, run)
	defer func() { b.stopRuntime(1); b.probeWG.Wait() }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("probe never started")
	}
	b.stopRuntime(1)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel HTTP request")
	}
	b.probeWG.Wait()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.publicLatency[1] != nil {
		t.Fatal("stopped probe retained result")
	}
}
