package cloudsync

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"loris-tunnel/internal/conf"
)

func TestDisabledDefaultsAndDormantLoop(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "legacy"}[legacy], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), DevSettingsFileName)
			if legacy {
				if err := os.WriteFile(path, []byte(`{"server_url":"http://127.0.0.1:8000","space_id":"space","automatic":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var snapshots, requests atomic.Int32
			s, err := New(path, "device", "Office", accountVault{}, Hooks{Snapshot: func() ([]byte, error) { snapshots.Add(1); return []byte("version=1\n"), nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			s.client.Transport = handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) })}
			s.Start()
			for i := 0; i < 10; i++ {
				s.Notify()
				s.Focus()
			}
			time.Sleep(1100 * time.Millisecond)
			if st := s.Status(); st.Enabled || st.State != "disabled" || snapshots.Load() != 0 || requests.Load() != 0 {
				t.Fatal(st, snapshots.Load(), requests.Load())
			}
			token := strings.Repeat("a", 43)
			checks := []func() error{
				func() error { _, e := s.Sync(); return e },
				func() error { _, e := s.Resolve("cloud", 1, "hash"); return e },
				func() error { _, e := s.Connect("http://127.0.0.1:8000", token, "Office"); return e },
				func() error { _, e := s.Test("http://127.0.0.1:8000", token); return e },
				func() error { _, e := s.GenerateToken("http://127.0.0.1:8000", "Office"); return e },
				func() error { _, e := s.Token(); return e },
				func() error { return s.SetPreferences(true, "Office") }, s.Disconnect,
			}
			for _, check := range checks {
				if err := check(); err == nil || !strings.Contains(err.Error(), "disabled") {
					t.Fatal("disabled action accepted", err)
				}
			}
			if requests.Load() != 0 || snapshots.Load() != 0 {
				t.Fatal("disabled operation did work")
			}
		})
	}
}

func TestUnconnectedEnabledLoopIsDormant(t *testing.T) {
	var snapshots atomic.Int32
	s, err := New(filepath.Join(t.TempDir(), DevSettingsFileName), "device", "Office", accountVault{}, Hooks{Snapshot: func() ([]byte, error) { snapshots.Add(1); return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if _, err = s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	s.Start()
	s.Notify()
	s.Focus()
	time.Sleep(1100 * time.Millisecond)
	if snapshots.Load() != 0 {
		t.Fatal("unconnected service scans configuration")
	}
}

func TestTogglePreservesConnectionAndRestartsWithExplicitState(t *testing.T) {
	s, _, l := readyFixture(t)
	if err := s.SetPreferences(true, "Office"); err != nil {
		t.Fatal(err)
	}
	before := s.options
	token, _ := s.Token()
	for i := 0; i < 3; i++ {
		if st, err := s.SetEnabled(false); err != nil || st.Enabled || st.State != "disabled" {
			t.Fatal(st, err)
		}
		if s.options.Baseline != before.Baseline || !s.options.Automatic || s.options.ServerURL != before.ServerURL {
			t.Fatal("toggle lost connection")
		}
		if st, err := s.SetEnabled(true); err != nil || !st.Enabled {
			t.Fatal(st, err)
		}
		if actual, _ := s.Token(); actual != token {
			t.Fatal("toggle removed token")
		}
	}
	for _, enabled := range []bool{false, true} {
		if _, err := s.SetEnabled(enabled); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(s.path)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil || fields["enabled"] == nil {
			t.Fatal("missing explicit enabled", err)
		}
		// Simulate a directory move: the credential account and enabled state travel together.
		moved := filepath.Join(t.TempDir(), ProdSettingsFileName)
		if err := os.WriteFile(moved, data, 0600); err != nil {
			t.Fatal(err)
		}
		reopened, err := New(moved, "device", "Office", s.vault, Hooks{Snapshot: l.snapshot, Apply: l.apply})
		if err != nil {
			t.Fatal(err)
		}
		if reopened.Status().Enabled != enabled {
			t.Fatal("restart lost enabled state")
		}
		if enabled {
			if actual, _ := reopened.Token(); actual != token {
				t.Fatal("directory move lost token")
			}
		}
		reopened.Close()
	}
	if err := s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if !s.Status().Enabled || s.Status().Connected || s.options.Baseline.SpaceID != "" {
		t.Fatal("disconnect lost enabled or kept connection")
	}
	if value, _ := s.vault.Get(s.account); value != "" {
		t.Fatal("disconnect kept credential")
	}
}

func TestTogglePersistenceFailureAndSuspension(t *testing.T) {
	s, _, _ := readyFixture(t)
	goodPath := s.path
	s.path = t.TempDir() // Atomic replacement of a directory must fail, even as root.
	if _, err := s.SetEnabled(false); err == nil || !s.Status().Enabled {
		t.Fatal("failed write disabled service", err)
	}
	s.path = goodPath
	s.suspended = true
	s.options.Automatic = true
	if _, err := s.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !s.suspended || s.Status().Automatic {
		t.Fatal("toggle bypassed unsafe baseline suspension")
	}
}

func TestDisableWaitsForRequestAndRejectsStaleConfirmation(t *testing.T) {
	s, c, _ := readyFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	s.client.Transport = handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; c.handler(w, r) })}
	synced := make(chan error, 1)
	go func() { _, err := s.Sync(); synced <- err }()
	<-entered
	disabled := make(chan error, 1)
	go func() { _, err := s.SetEnabled(false); disabled <- err }()
	select {
	case err := <-disabled:
		t.Fatal("disable returned before active request", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-synced; err != nil {
		t.Fatal(err)
	}
	if err := <-disabled; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve("cloud", 1, "hash"); err == nil {
		t.Fatal("late confirmation accepted")
	}
	if _, err := s.Sync(); err == nil {
		t.Fatal("late sync accepted")
	}
}

func TestReenableManualAndAutomaticModes(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "automatic"}[automatic], func(t *testing.T) {
			s, c, l := readyFixture(t)
			if err := s.SetPreferences(automatic, "Office"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SetEnabled(false); err != nil {
				t.Fatal(err)
			}
			var requests, snapshots atomic.Int32
			s.hooks.Snapshot = func() ([]byte, error) { snapshots.Add(1); return l.snapshot() }
			s.client.Transport = handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); c.handler(w, r) })}
			s.Start()
			l.edit("# changed while disabled\nversion=2\n")
			if _, err := s.SetEnabled(true); err != nil {
				t.Fatal(err)
			}
			if automatic {
				deadline := time.Now().Add(time.Second)
				for time.Now().Before(deadline) {
					c.mu.Lock()
					puts := c.puts
					c.mu.Unlock()
					if puts == 2 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				c.mu.Lock()
				puts := c.puts
				c.mu.Unlock()
				if puts != 2 {
					t.Fatal("automatic mode did not resume")
				}
			} else {
				time.Sleep(100 * time.Millisecond)
				if requests.Load() != 0 {
					t.Fatal("manual enable accessed server")
				}
			}
			if snapshots.Load() == 0 {
				t.Fatal("enable did not refresh fingerprint")
			}
			if _, err := s.SetEnabled(false); err != nil {
				t.Fatal(err)
			}
			count := snapshots.Load()
			network := requests.Load()
			for i := 0; i < 10; i++ {
				s.Focus()
				s.Notify()
			}
			time.Sleep(1100 * time.Millisecond)
			if snapshots.Load() != count || requests.Load() != network {
				t.Fatal("work continued after disable returned")
			}
		})
	}
}

func TestLegacyMigrationRespectsExplicitToggleAndDisconnect(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, SettingsFileName)
	target := filepath.Join(dir, ProdSettingsFileName)
	server := "https://sync.example.com"
	if err := (&Service{path: legacy}).persist(Options{ServerURL: server, SpaceID: "space"}); err != nil {
		t.Fatal(err)
	}
	for _, options := range []Options{{Enabled: false}, {Enabled: true}, {CredentialAccount: "saved-account"}} {
		if err := (&Service{path: target}).persist(options); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(target)
		if err := MigrateLegacy(target, legacy, "device", server, conf.SyncBaseline{}); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(target)
		if string(before) != string(after) {
			t.Fatal("legacy settings overwrote explicit choice")
		}
	}
}
