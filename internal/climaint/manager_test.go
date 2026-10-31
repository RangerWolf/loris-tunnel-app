package climaint

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"loris-tunnel/internal/updater"
)

func testManager(t *testing.T, initial string, external bool) *Manager {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, "bin", "cli")
	original := target
	if external {
		original = filepath.Join(root, "system", "cli")
	}
	if initial != "" {
		if err := writeExecutable(original, []byte(initial)); err != nil {
			t.Fatal(err)
		}
	}
	spec := Spec{Provider: "cloudflare", Target: target, Unpack: func(path, dir string) (string, error) { return path, nil }}
	spec.Resolve = func() (string, string, error) {
		path, chosen, err := SelectedPath(target)
		if err != nil {
			return "", "", err
		}
		if !chosen {
			path = original
		}
		kind := "managed"
		if path != target {
			kind = "system"
		}
		_, err = os.Stat(path)
		return path, kind, err
	}
	spec.Inspect = func(ctx context.Context, path string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		version := strings.TrimSpace(string(raw))
		if !regexp.MustCompile(`^\d+\.\d+\.\d+(?:[-+][\w.-]+)?$`).MatchString(version) {
			return "", fmt.Errorf("wrong executable")
		}
		return version, nil
	}
	return New(spec)
}
func allow() (func(), error) { return func() {}, nil }
func imported(t *testing.T, m *Manager, version string) Status {
	t.Helper()
	path := filepath.Join(t.TempDir(), "import-cli")
	if err := writeExecutable(path, []byte(version)); err != nil {
		t.Fatal(err)
	}
	status, err := m.Import(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	return status
}
func assertCurrent(t *testing.T, m *Manager, want string) {
	t.Helper()
	status := m.Status(t.Context())
	if status.CurrentVersion != want {
		t.Fatalf("current=%s want=%s error=%s", status.CurrentVersion, want, status.Error)
	}
}
func TestUpgradeRestoreAndOnePreviousVersion(t *testing.T) {
	m := testManager(t, "1.0.0", false)
	for _, version := range []string{"1.1.0", "1.2.0"} {
		chosen := imported(t, m, version)
		if _, err := m.Apply(t.Context(), chosen.CandidateID, allow); err != nil {
			t.Fatal(err)
		}
		assertCurrent(t, m, version)
	}
	status := m.Status(t.Context())
	if status.PreviousVersion != "1.1.0" || !status.CanRestore {
		t.Fatal(status)
	}
	files, _ := filepath.Glob(filepath.Join(directory(m.Spec.Target), "saved-*"))
	if len(files) != 1 {
		t.Fatal("more than one previous executable retained", files)
	}
	if _, err := m.Restore(t.Context(), allow); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, m, "1.1.0")
	if _, err := m.Restore(t.Context(), allow); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, m, "1.2.0")
	// The source selection and backup survive restarting the manager.
	restarted := New(m.Spec)
	assertCurrent(t, restarted, "1.2.0")
	if restarted.Status(t.Context()).PreviousVersion != "1.1.0" {
		t.Fatal("previous version was not persisted")
	}
}
func TestExternalSourceRestoration(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			m := testManager(t, "1.0.0", true)
			original, _, _ := m.Spec.Resolve()
			selected := imported(t, m, "2.0.0")
			if _, err := m.Apply(t.Context(), selected.CandidateID, allow); err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(original)
			if string(raw) != "1.0.0" {
				t.Fatal("external executable overwritten")
			}
			if changed {
				if err := os.WriteFile(original, []byte("3.0.0"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			status, err := m.Restore(t.Context(), allow)
			if err != nil {
				t.Fatal(err)
			}
			if status.CurrentVersion != "1.0.0" {
				t.Fatal(status)
			}
			if changed && status.Source != "managed" || !changed && status.Path != original {
				t.Fatal("wrong source after restore", status)
			}
			raw, _ = os.ReadFile(original)
			want := "1.0.0"
			if changed {
				want = "3.0.0"
			}
			if string(raw) != want {
				t.Fatal("restore modified external file")
			}
		})
	}
}
func TestReplacementFailureKeepsWorkingCurrentAndPrevious(t *testing.T) {
	m := testManager(t, "1.0.0", false)
	first := imported(t, m, "1.1.0")
	if _, err := m.Apply(t.Context(), first.CandidateID, allow); err != nil {
		t.Fatal(err)
	}
	second := imported(t, m, "1.2.0")
	m.beforeCommit = func() error { return errors.New("simulated state write failure") }
	if _, err := m.Apply(t.Context(), second.CandidateID, allow); err == nil {
		t.Fatal("expected failure")
	}
	assertCurrent(t, m, "1.1.0")
	state, _ := loadSelection(m.Spec.Target)
	if state.Previous.Version != "1.0.0" {
		t.Fatal("previous backup was replaced by failed upgrade")
	}
	if _, err := snapshotBytes(m.Spec.Target, state.Previous); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journalPath(m.Spec.Target)); !os.IsNotExist(err) {
		t.Fatal("completed rollback left a transaction")
	}
}
func TestCrashRecoveryAndDamagedBackup(t *testing.T) {
	m := testManager(t, "1.0.0", false)
	selected := imported(t, m, "2.0.0")
	if _, err := m.Apply(t.Context(), selected.CandidateID, allow); err != nil {
		t.Fatal(err)
	}
	before, _ := loadSelection(m.Spec.Target)
	saved, err := saveSnapshot(m.Spec.Target, m.Spec.Target, "", "managed")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(journalPath(m.Spec.Target), journal{Before: before, TargetExisted: true, SavedTarget: saved}); err != nil {
		t.Fatal(err)
	}
	if err := writeExecutable(m.Spec.Target, []byte("interrupted update")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SelectedPath(m.Spec.Target); err != nil {
		t.Fatal(err)
	}
	assertCurrent(t, m, "2.0.0")
	if err := os.WriteFile(filepath.Join(directory(m.Spec.Target), before.Previous.File), []byte("damaged"), 0700); err != nil {
		t.Fatal(err)
	}
	if status := m.Status(t.Context()); status.CanRestore {
		t.Fatal("damaged backup offered as restorable")
	}
	if _, err := m.Restore(t.Context(), allow); err == nil {
		t.Fatal("damaged backup restored")
	}
	assertCurrent(t, m, "2.0.0")
}
func TestPostReplacementValidationFailureRollsBack(t *testing.T) {
	m := testManager(t, "1.0.0", false)
	chosen := imported(t, m, "2.0.0")
	inspect := m.Spec.Inspect
	m.Spec.Inspect = func(ctx context.Context, path string) (string, error) {
		if path == m.Spec.Target {
			raw, _ := os.ReadFile(path)
			if string(raw) == "2.0.0" {
				return "", errors.New("installed executable failed validation")
			}
		}
		return inspect(ctx, path)
	}
	if _, err := m.Apply(t.Context(), chosen.CandidateID, allow); err == nil {
		t.Fatal("expected validation failure")
	}
	assertCurrent(t, m, "1.0.0")
}
func TestDownloadCancelAndImportFailuresDoNotChangeCurrent(t *testing.T) {
	m := testManager(t, "1.0.0", false)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	m.Spec.DirectURL = server.URL
	done := make(chan error, 1)
	go func() { _, err := m.Check(context.Background(), true); done <- err }()
	<-started
	m.Cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not finish")
	}
	assertCurrent(t, m, "1.0.0")
	bad := filepath.Join(t.TempDir(), "bad")
	_ = os.WriteFile(bad, []byte("<html>not a CLI</html>"), 0600)
	if _, err := m.Import(t.Context(), bad); err == nil {
		t.Fatal("wrong executable accepted")
	}
	assertCurrent(t, m, "1.0.0")
	selected := imported(t, m, "2.0.0")
	if _, err := m.Apply(t.Context(), selected.CandidateID, func() (func(), error) { return nil, errors.New("tunnel running") }); err == nil {
		t.Fatal("guard bypassed")
	}
	assertCurrent(t, m, "1.0.0")
}
func TestDevTunnelETagCacheAndChangedPackage(t *testing.T) {
	var downloads atomic.Int32
	var changed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := "1.1.0"
		tag := "one"
		if changed.Load() {
			version = "1.2.0"
			tag = "two"
		}
		w.Header().Set("ETag", tag)
		if r.Method == http.MethodHead {
			return
		}
		downloads.Add(1)
		_, _ = w.Write([]byte(version))
	}))
	defer server.Close()
	m := testManager(t, "1.0.0", false)
	m.Spec.Provider = "devtunnel"
	m.Spec.DirectURL = server.URL
	status, err := m.Check(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !status.HasUpdate || status.LatestVersion != "1.1.0" {
		t.Fatal(status)
	}
	if _, err = m.Check(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 1 {
		t.Fatal("unchanged official package downloaded again")
	}
	changed.Store(true)
	status, err = m.Check(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 2 || status.LatestVersion != "1.2.0" {
		t.Fatal(status, downloads.Load())
	}
	files, _ := filepath.Glob(filepath.Join(directory(m.Spec.Target), "candidate-*"))
	if len(files) != 1 {
		t.Fatal("old candidates accumulated", files)
	}
	if _, err = m.Check(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 2 {
		t.Fatal("fresh cache bypassed")
	}
}
func TestReleaseChecksumsPinnedVersionAndFailureStates(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			m := testManager(t, "1.0.0", false)
			m.Spec.Provider = "frp"
			asset, _ := assetName("frp", "1.1.0")
			var base string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/latest":
					fmt.Fprintf(w, `{"tag_name":"v1.1.0","assets":[{"name":%q,"browser_download_url":%q},{"name":"frp_sha256_checksums.txt","browser_download_url":%q}]}`, asset, base+"/archive", base+"/checksums")
				case "/archive":
					_, _ = w.Write([]byte("1.1.0"))
				case "/checksums":
					sum := digest([]byte("1.1.0"))
					if bad {
						sum = digest([]byte("tampered"))
					}
					fmt.Fprintf(w, "%s  %s\n", sum, asset)
				}
			}))
			defer server.Close()
			base = server.URL
			m.Spec.ReleaseAPI = base + "/latest"
			chosen, err := m.Check(t.Context(), true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.Apply(t.Context(), chosen.CandidateID, allow)
			if bad {
				if err == nil {
					t.Fatal("bad checksum accepted")
				}
				assertCurrent(t, m, "1.0.0")
			} else {
				if err != nil {
					t.Fatal(err)
				}
				assertCurrent(t, m, "1.1.0")
				if m.Status(t.Context()).HasUpdate {
					t.Fatal("updated version still offered as upgrade")
				}
			}
		})
	}
	for _, code := range []int{403, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
			defer server.Close()
			m := testManager(t, "1.0.0", false)
			m.Spec.ReleaseAPI = server.URL
			if _, err := m.Check(t.Context(), true); err == nil {
				t.Fatal("HTTP failure reported as up to date")
			}
			assertCurrent(t, m, "1.0.0")
		})
	}
}
func TestUnknownVersionsAndNoAutomaticDowngrade(t *testing.T) {
	for _, pair := range [][2]string{{"2026.10.10", "2026.10.3"}, {"1.10.0", "1.9.0"}, {"1.0.2115+e16833f284", "1.0.2100"}, {"1.0.1+build.9", "1.0.0"}, {"1.0.0", "1.0.0-rc.1"}, {"1.0.0-rc.10", "1.0.0-rc.2"}} {
		if cmp, known := updater.CompareVersions(pair[0], pair[1]); !known || cmp <= 0 {
			t.Fatal(pair, cmp, known)
		}
	}
	if _, known := updater.CompareVersions("unknown", "1.0.0"); known {
		t.Fatal("unknown version compared")
	}
	m := testManager(t, "2.0.0", false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("1.0.0")) }))
	defer server.Close()
	m.Spec.DirectURL = server.URL
	chosen, err := m.Check(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if chosen.HasUpdate {
		t.Fatal("downgrade advertised")
	}
	if _, err := m.Apply(t.Context(), chosen.CandidateID, allow); err == nil {
		t.Fatal("automatic downgrade applied")
	}
	assertCurrent(t, m, "2.0.0")
}

func TestInvalidReleaseMetadataAndWebDownload(t *testing.T) {
	for _, body := range []string{`{"tag_name":"v2.0.0","prerelease":true}`, `{"tag_name":"v2.0.0","draft":true}`, `{"tag_name":"v2.0.0","assets":[]}`, `<html>error</html>`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			m := testManager(t, "1.0.0", false)
			m.Spec.ReleaseAPI = server.URL
			if _, err := m.Check(t.Context(), true); err == nil {
				t.Fatal("invalid release accepted")
			}
			assertCurrent(t, m, "1.0.0")
		})
	}
	m := testManager(t, "1.0.0", false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>download error</html>"))
	}))
	defer server.Close()
	m.Spec.Provider = "devtunnel"
	m.Spec.DirectURL = server.URL
	if _, err := m.Check(t.Context(), true); err == nil {
		t.Fatal("web page accepted as a CLI")
	}
	assertCurrent(t, m, "1.0.0")
}
func TestStaleAndTamperedCandidatesCannotReplace(t *testing.T) {
	m := testManager(t, "1.0.0", false)
	old := imported(t, m, "1.1.0")
	current := imported(t, m, "1.2.0")
	if _, err := m.Apply(t.Context(), old.CandidateID, allow); err == nil {
		t.Fatal("stale candidate applied")
	}
	var item candidate
	if err := readJSON(m.candidatePath(), &item); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory(m.Spec.Target), item.File), []byte("1.9.0"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(t.Context(), current.CandidateID, allow); err == nil {
		t.Fatal("tampered candidate applied")
	}
	assertCurrent(t, m, "1.0.0")
}
func TestFirstInstallFailureAndUnrecoverableJournal(t *testing.T) {
	m := testManager(t, "", false)
	chosen := imported(t, m, "1.0.0")
	m.beforeCommit = func() error { return errors.New("state write failed") }
	if _, err := m.Apply(t.Context(), chosen.CandidateID, allow); err == nil {
		t.Fatal("first installation failure hidden")
	}
	if _, err := os.Stat(m.Spec.Target); !os.IsNotExist(err) {
		t.Fatal("failed first installation left executable")
	}
	m.beforeCommit = nil
	if _, err := m.Apply(t.Context(), chosen.CandidateID, allow); err != nil {
		t.Fatal(err)
	}
	saved, err := saveSnapshot(m.Spec.Target, m.Spec.Target, "1.0.0", "managed")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := loadSelection(m.Spec.Target)
	if err := writeJSON(journalPath(m.Spec.Target), journal{Before: before, TargetExisted: true, SavedTarget: saved}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory(m.Spec.Target), saved.File), []byte("damaged"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SelectedPath(m.Spec.Target); err == nil {
		t.Fatal("recovery failure hidden")
	}
	if _, err := os.Stat(journalPath(m.Spec.Target)); err != nil {
		t.Fatal("failed recovery discarded journal")
	}
	if _, err := os.Stat(filepath.Join(directory(m.Spec.Target), saved.File)); err != nil {
		t.Fatal("failed recovery discarded backup")
	}
}

func TestCancellationCannotInterruptReplacement(t *testing.T) {
	m := testManager(t, "1.0.0", false)
	chosen := imported(t, m, "2.0.0")
	m.beforeCommit = func() error {
		if m.Cancel() {
			t.Fatal("critical replacement accepted cancellation")
		}
		return nil
	}
	status, err := m.Apply(t.Context(), chosen.CandidateID, allow)
	if err != nil || status.Phase != "updated" {
		t.Fatal(status, err)
	}
	assertCurrent(t, m, "2.0.0")
}
