package cloudsync

import (
	"encoding/json"
	"fmt"
	"loris-tunnel/internal/conf"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Legacy migration tests reuse this raw-file hook without persisting configuration metadata.
type localStore struct{ c *conf.Config }

func (l *localStore) hooks() Hooks {
	return Hooks{Snapshot: func() ([]byte, error) { return conf.ExportTOML(l.c), nil }}
}

type fileStore struct {
	mu   sync.Mutex
	data []byte
	fail bool
}

func (l *fileStore) snapshot() ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]byte(nil), l.data...), nil
}
func (l *fileStore) apply(data []byte, expected string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail {
		return fmt.Errorf("write failed")
	}
	if Fingerprint(l.data) != expected {
		return fmt.Errorf("changed")
	}
	l.data = append([]byte(nil), data...)
	return nil
}
func (l *fileStore) edit(data string) { l.mu.Lock(); defer l.mu.Unlock(); l.data = []byte(data) }

type fileCloud struct {
	mu       sync.Mutex
	revision int64
	config   *Config
	puts     int
}

func (c *fileCloud) handler(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Method == "PUT" {
		var p struct {
			BaseRevision int64   `json:"base_revision"`
			DeviceName   string  `json:"device_name"`
			Config       *Config `json:"config"`
		}
		if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
			w.WriteHeader(422)
			return
		}
		if p.BaseRevision != c.revision {
			w.WriteHeader(409)
			return
		}
		c.revision++
		c.config = p.Config
		c.puts++
	}
	remote := Remote{SpaceID: "space", Revision: c.revision, SchemaVersion: SchemaVersion}
	if strings.HasSuffix(r.URL.Path, "/config") && r.Method == "GET" {
		remote.Config = c.config
	}
	json.NewEncoder(w).Encode(remote)
}
func serviceFixture(t *testing.T, c *fileCloud, l *fileStore) (*Service, string) {
	t.Helper()
	s, e := New(filepath.Join(t.TempDir(), DevSettingsFileName), "device", "Office", accountVault{}, Hooks{Snapshot: l.snapshot, Apply: l.apply})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	s.client.Transport = handlerTransport{http.HandlerFunc(c.handler)}
	return s, "http://127.0.0.1:8000"
}
func readyFixture(t *testing.T) (*Service, *fileCloud, *fileStore) {
	t.Helper()
	c := &fileCloud{}
	l := &fileStore{data: []byte("# original\nversion=1\n")}
	s, url := serviceFixture(t, c, l)
	st, e := s.Connect(url, strings.Repeat("a", 43), "Office")
	if e != nil || st.State != "synced" || c.puts != 1 {
		t.Fatal(st, e)
	}
	return s, c, l
}
func TestWholeFileBackupsWithTokenOnly(t *testing.T) {
	s, c, l := readyFixture(t)
	if _, e := s.Sync(); e != nil || c.puts != 1 {
		t.Fatal("unchanged file duplicated", e)
	}
	settings, _ := os.ReadFile(s.path)
	if strings.Contains(string(settings), strings.Repeat("a", 43)) || strings.Contains(string(settings), "recovery") {
		t.Fatal("secret or recovery metadata in JSON")
	}
	l.edit("# changed comment\nversion=1\n")
	if _, e := s.Sync(); e != nil || c.puts != 2 {
		t.Fatal("raw edit ignored", e)
	}
	if e := s.Disconnect(); e != nil {
		t.Fatal(e)
	}
}
func TestCloudRestoreConflictAndStaleConfirmation(t *testing.T) {
	s, c, l := readyFixture(t)
	remoteData := []byte("# cloud\nversion=2\n")
	sealed, _ := Pack(remoteData)
	c.mu.Lock()
	c.revision = 2
	c.config = sealed
	c.mu.Unlock()
	st, e := s.Sync()
	if e != nil || st.State != "update_available" {
		t.Fatal(st, e)
	}
	l.edit("# external edit\nversion=3\n")
	if _, e = s.Resolve("cloud", st.Revision, st.LocalFingerprint); e != nil {
		t.Fatal(e)
	}
	current, _ := l.snapshot()
	if string(current) == string(remoteData) {
		t.Fatal("stale confirmation overwrote edit")
	}
	st = s.Status()
	if st.State != "conflict" {
		t.Fatal(st)
	}
	l.fail = true
	if _, e = s.Resolve("cloud", st.Revision, st.LocalFingerprint); e == nil {
		t.Fatal("write failure hidden")
	}
	current, _ = l.snapshot()
	if string(current) == string(remoteData) {
		t.Fatal("failed write modified file")
	}
	l.fail = false
	s.retryAt = time.Time{}
	st, e = s.Sync()
	if e != nil {
		t.Fatal(e)
	}
	st, e = s.Resolve("cloud", st.Revision, st.LocalFingerprint)
	if e != nil || st.State != "synced" {
		t.Fatal(st, e)
	}
	current, _ = l.snapshot()
	if string(current) != string(remoteData) {
		t.Fatal("restore changed raw bytes")
	}
	if _, e = s.Sync(); e != nil || c.puts != 1 {
		t.Fatal("restore reuploaded", e)
	}
	c.mu.Lock()
	c.revision = 1
	c.mu.Unlock()
	if _, e = s.Sync(); e == nil {
		t.Fatal("rollback accepted")
	}
}
func TestNewDeviceConnectsWithTokenAndConfirmsOverwrite(t *testing.T) {
	_, c, _ := readyFixture(t)
	l := &fileStore{data: []byte("version=9\n")}
	s, url := serviceFixture(t, c, l)
	st, e := s.Connect(url, strings.Repeat("a", 43), "New device")
	if e != nil || st.State != "conflict" {
		t.Fatal(st, e)
	}
	st, e = s.Resolve("cloud", st.Revision, st.LocalFingerprint)
	if e != nil || st.State != "synced" {
		t.Fatal(st, e)
	}
	data, _ := l.snapshot()
	if string(data) != "# original\nversion=1\n" {
		t.Fatal("file restore failed")
	}
}
func TestBaselineFailureSuspendsAutomaticBackup(t *testing.T) {
	s, c, l := readyFixture(t)
	cloudData := []byte("version=4\n")
	sealed, _ := Pack(cloudData)
	c.revision = 2
	c.config = sealed
	st, e := s.Sync()
	if e != nil {
		t.Fatal(e)
	}
	s.options.Automatic = true
	badPath := filepath.Join(t.TempDir(), "directory")
	os.Mkdir(badPath, 0700)
	s.path = badPath
	if _, e = s.Resolve("cloud", st.Revision, st.LocalFingerprint); e == nil {
		t.Fatal("baseline failure hidden")
	}
	data, _ := l.snapshot()
	if string(data) != string(cloudData) || !s.suspended || s.Status().Automatic {
		t.Fatal("unsafe partial restore")
	}
	if e = s.SetPreferences(true, "Office"); e == nil {
		t.Fatal("automation resumed without baseline")
	}
	s.path = filepath.Join(t.TempDir(), DevSettingsFileName)
	s.retryAt = time.Time{}
	if _, e = s.Sync(); e != nil || s.suspended {
		t.Fatal("baseline retry failed", e)
	}
}
func TestDirectFileEditTriggersAutomaticBackup(t *testing.T) {
	s, c, l := readyFixture(t)
	if e := s.SetPreferences(true, "Office"); e != nil {
		t.Fatal(e)
	}
	s.Start()
	l.edit("# edited directly\nversion=7\n")
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		puts := c.puts
		c.mu.Unlock()
		if puts == 2 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("external file edit was not backed up")
}

type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
