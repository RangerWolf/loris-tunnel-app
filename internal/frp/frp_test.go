package frp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"loris-tunnel/internal/forward"
)

// The Go test executable doubles as a portable frpc fixture, avoiding shell scripts.
func TestMain(m *testing.M) {
	if os.Getenv("LORIS_FRP_TEST_CHILD") == "1" && len(os.Args) > 1 && os.Args[1] == "stop" {
		var cfg struct {
			WebServer struct {
				Addr           string
				Port           int
				User, Password string
			}
		}
		if len(os.Args) < 4 {
			os.Exit(6)
		}
		if _, err := toml.DecodeFile(os.Args[3], &cfg); err != nil {
			os.Exit(6)
		}
		req, _ := http.NewRequest("POST", fmt.Sprintf("http://%s:%d/api/stop", cfg.WebServer.Addr, cfg.WebServer.Port), nil)
		req.SetBasicAuth(cfg.WebServer.User, cfg.WebServer.Password)
		client := &http.Client{Timeout: time.Second}
		resp, err := client.Do(req)
		if err != nil {
			os.Exit(7)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(8)
		}
		os.Exit(0)
	}

	if os.Getenv("LORIS_FRP_TEST_CHILD") == "1" && len(os.Args) == 3 && os.Args[1] == "-c" {
		var cfg struct {
			User      string
			Proxies   []struct{ Name string }
			WebServer struct {
				Addr           string
				Port           int
				User, Password string
			}
		}
		if _, err := toml.DecodeFile(os.Args[2], &cfg); err != nil {
			os.Exit(3)
		}
		if marker := os.Getenv("LORIS_FRP_TEST_READY"); marker != "" {
			if err := os.WriteFile(marker, []byte("ready"), 0600); err != nil {
				os.Exit(5)
			}
		}
		name := cfg.Proxies[0].Name
		if cfg.User != "" {
			name = cfg.User + "." + name
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || u != cfg.WebServer.User || p != cfg.WebServer.Password {
				w.WriteHeader(401)
				return
			}
			if marker := os.Getenv("LORIS_FRP_TEST_STOPPED"); marker != "" {
				_ = os.WriteFile(marker, []byte("stopped"), 0600)
			}
			w.WriteHeader(200)
			go func() { time.Sleep(50 * time.Millisecond); os.Exit(0) }()
		})
		mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
			user, password, ok := r.BasicAuth()
			if !ok || user != cfg.WebServer.User || password != cfg.WebServer.Password {
				w.WriteHeader(401)
				return
			}
			state, _ := os.ReadFile(os.Getenv("LORIS_FRP_TEST_STATE"))
			if string(state) == "offline" {
				json.NewEncoder(w).Encode(map[string]any{})
				return
			}
			if string(state) == "exists" {
				json.NewEncoder(w).Encode(map[string]any{"tcp": []map[string]string{{"name": name, "status": "start error", "err": "proxy [" + name + "] already exists"}}})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"tcp": []map[string]string{{"name": name, "status": "running"}}})
		})
		if err := http.ListenAndServe(net.JoinHostPort(cfg.WebServer.Addr, fmt.Sprint(cfg.WebServer.Port)), mux); err != nil {
			os.Exit(4)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func testServer() Server {
	return Server{Name: "local", Host: "127.0.0.1", Port: 7000, Token: "secret-token", HTTPPort: 80, TLS: true}
}
func TestConfigurationAndValidation(t *testing.T) {
	server := testServer()
	server.Token = "quoted\"\nvalue"
	data, err := configuration(server, "web", "http", "127.0.0.1", 3000, 0, "dev.example.com", 7400, "admin-secret")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Auth    struct{ Token string }
		Proxies []struct {
			Type          string
			CustomDomains []string
		}
		WebServer struct{ Addr, Password string }
		Transport struct{ TLS struct{ Enable bool } }
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Token != server.Token || cfg.WebServer.Addr != "127.0.0.1" || !cfg.Transport.TLS.Enable || cfg.Proxies[0].CustomDomains[0] != "dev.example.com" {
		t.Fatalf("incorrect config: %+v", cfg)
	}
	for _, domain := range []string{"https://example.com", "example.com/path", "example.com:80", "bad domain", "127.0.0.1"} {
		if ValidateProxy("http", "127.0.0.1", 3000, 0, domain) == nil {
			t.Errorf("accepted %q", domain)
		}
	}
	if ValidateProxy("tcp", "::1", 3000, 13000, "") != nil || ValidateProxy("tcp", "127.0.0.1", 3000, 0, "") == nil || ValidateProxy("udp", "localhost", 3000, 13000, "") == nil {
		t.Fatal("invalid type/port validation")
	}
	server.PublicHost = "::1"
	if Address(server, "tcp", 13000, "") != "[::1]:13000" {
		t.Fatal("IPv6 address")
	}
	server.HTTPPort = 8080
	if Address(server, "http", 0, "dev.example.com") != "http://dev.example.com:8080" {
		t.Fatal("HTTP entry port")
	}
	if DomainName("http://http://dev.example.com:8080/path") != "dev.example.com" {
		t.Fatal("domain name")
	}
}
func TestStoreKeepsSecretsAndPreservesBlankToken(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "frp-local.json")}
	server, err := store.SaveServer(testServer())
	if err != nil {
		t.Fatal(err)
	}
	if server.Token != "" || !server.HasToken || server.ID == "" {
		t.Fatal("secret returned to UI")
	}
	cfg, err := store.Settings()
	if err != nil || cfg.Servers[0].Token != "" {
		t.Fatal("secret in settings")
	}
	server.Name = "renamed"
	if _, err = store.SaveServer(server); err != nil {
		t.Fatal(err)
	}
	secret, err := store.Server(server.ID)
	if err != nil || secret.Token != "secret-token" {
		t.Fatal("blank update lost token")
	}
	if _, err = store.SaveServer(Server{ID: "stale", Name: "other", Host: "localhost"}); err == nil {
		t.Fatal("stale ID recreated")
	}
	server.ClearToken = true
	if _, err = store.SaveServer(server); err != nil {
		t.Fatal(err)
	}
	cleared, err := store.Server(server.ID)
	if err != nil || cleared.Token != "" {
		t.Fatal("explicit clear kept token")
	}
	if err = store.DeleteServer(server.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Server(server.ID); err == nil {
		t.Fatal("deleted server found")
	}
}
func TestStatusRequiresMatchingRegisteredProxy(t *testing.T) {
	var state atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "loris" || password != "password" {
			t.Error("missing API authentication")
		}
		switch state.Load() {
		case 0:
			fmt.Fprint(w, `{"tcp":[{"name":"other","status":"running"}]}`)
		case 1:
			fmt.Fprint(w, `{"tcp":[{"name":"web","status":"running"}]}`)
		case 2:
			fmt.Fprint(w, `{"tcp":[{"name":"web","status":"error","err":"token secret-token password"}]}`)
		}
	}))
	defer api.Close()
	host := &Host{endpoint: api.URL, password: "password", name: "web", token: "secret-token"}
	if ready, _ := host.status(context.Background()); ready {
		t.Fatal("unrelated proxy accepted")
	}
	state.Store(1)
	if ready, err := host.status(context.Background()); !ready || err != nil {
		t.Fatal(err)
	}
	state.Store(2)
	_, err := host.status(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "password") {
		t.Fatalf("unredacted error: %v", err)
	}
}
func TestRuntimeLifecycleReconnectAndCleanup(t *testing.T) {
	stoppedMarker := filepath.Join(t.TempDir(), "stopped")
	t.Setenv("LORIS_FRP_TEST_STOPPED", stoppedMarker)
	t.Setenv("LORIS_FRP_TEST_CHILD", "1")
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	t.Setenv("LORIS_FRP_TEST_STATE", state)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Store: &Store{Path: filepath.Join(dir, "frp-local.json")}}
	if err = client.Store.SetBinary(binary); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer origin.Close()
	port := origin.Listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	server := testServer()
	server.User = "alice"
	host, err := client.Start(ctx, server, "web", "tcp", "127.0.0.1", port, 13000, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Stop() })
	if host.OriginState() != "reachable" || host.PublicURL() != "127.0.0.1:13000" {
		t.Fatal("missing runtime data")
	}
	if err = os.WriteFile(state, []byte("offline"), 0600); err != nil {
		t.Fatal(err)
	}
	awaitEvent(t, host, forward.RuntimeEventDisconnected)
	if err = os.WriteFile(state, []byte("online"), 0600); err != nil {
		t.Fatal(err)
	}
	awaitEvent(t, host, forward.RuntimeEventReconnected)
	if err = host.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stoppedMarker); err != nil {
		t.Fatal("frpc stop was not used", err)
	}
	runs, _ := filepath.Glob(filepath.Join(dir, "frp", "run-*"))
	if len(runs) != 0 {
		t.Fatal("credential config left behind")
	}
	if host.Err() != nil {
		t.Fatal("manual stop reported as failure")
	}
	second, err := client.Start(ctx, server, "web", "tcp", "127.0.0.1", port, 13000, "")
	if err != nil {
		t.Fatal("restart after stop failed", err)
	}
	if err := second.Stop(); err != nil {
		t.Fatal(err)
	}
}
func awaitEvent(t *testing.T, h *Host, want forward.RuntimeEventType) {
	t.Helper()
	select {
	case event := <-h.Events():
		if event.Type != want {
			t.Fatalf("event=%v want=%v", event, want)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("runtime event timeout")
	}
}
func TestCanceledStartDoesNotLeaveProcessOrConfig(t *testing.T) {
	t.Setenv("LORIS_FRP_TEST_CHILD", "1")
	dir := t.TempDir()
	binary, _ := os.Executable()
	c := &Client{Store: &Store{Path: filepath.Join(dir, "local.json")}}
	c.Store.SetBinary(binary)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Start(ctx, testServer(), "web", "tcp", "127.0.0.1", 3000, 13000, ""); err == nil {
		t.Fatal("cancellation ignored")
	}
	runs, _ := filepath.Glob(filepath.Join(dir, "frp", "run-*"))
	if len(runs) != 0 {
		t.Fatal("config left behind")
	}
}
func TestFailureHintNeverReturnsRawOutput(t *testing.T) {
	hint := &failureHint{}
	hint.Write([]byte("secret-token: token in login doesn't "))
	hint.Write([]byte("match"))
	if hint.message() != "FRP authentication failed; check the server token" {
		t.Fatal(hint.message())
	}
}

func TestCancelWhileStartingCleansUp(t *testing.T) {
	t.Setenv("LORIS_FRP_TEST_CHILD", "1")
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.WriteFile(state, []byte("offline"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LORIS_FRP_TEST_STATE", state)
	marker := filepath.Join(dir, "ready")
	t.Setenv("LORIS_FRP_TEST_READY", marker)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Store: &Store{Path: filepath.Join(dir, "local.json")}}
	if err = c.Store.SetBinary(binary); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		h, err := c.Start(ctx, testServer(), "web", "tcp", "127.0.0.1", 3000, 13000, "")
		if h != nil {
			_ = h.Stop()
		}
		result <- err
	}()
	deadline := time.Now().Add(4 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled start succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled start did not finish")
	}
	runs, err := filepath.Glob(filepath.Join(dir, "frp", "run-*"))
	if err != nil || len(runs) != 0 {
		t.Fatalf("runtime config left behind: %v %v", runs, err)
	}
}
