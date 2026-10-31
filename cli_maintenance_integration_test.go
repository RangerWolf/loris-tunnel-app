package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"loris-tunnel/internal/climaint"
	"loris-tunnel/internal/cloudflared"
	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/frp"
)

// Opt-in network test: all downloads, selected executables and backups stay in TempDir.
// No real installed CLI, authentication data or tunnel configuration is replaced.
func TestCLIMaintenanceOfficialPackages(t *testing.T) {
	mode := os.Getenv("LORIS_CLI_OFFICIAL_TEST")
	if mode != "1" && mode != "pinned" {
		t.Skip("set LORIS_CLI_OFFICIAL_TEST=1 to verify official platform downloads in temporary directories")
	}
	specs := []climaint.Spec{cloudflared.NewClient("").MaintenanceSpec(), devtunnel.NewClient("").MaintenanceSpec(), (&frp.Client{Store: &frp.Store{Path: filepath.Join(t.TempDir(), "frp-local.json")}}).MaintenanceSpec()}
	for _, spec := range specs {
		t.Run(spec.Provider, func(t *testing.T) {
			if mode == "pinned" && spec.Provider != "devtunnel" {
				// A pinned integration run still downloads and executes real official
				// packages; only discovery metadata is local to avoid API rate limits.
				version, repo := "2026.10.3", "cloudflare/cloudflared"
				asset := "cloudflared-" + runtime.GOOS + "-" + runtime.GOARCH
				if runtime.GOOS == "darwin" {
					asset += ".tgz"
				}
				if runtime.GOOS == "windows" {
					asset += ".exe"
				}
				if spec.Provider == "frp" {
					version, repo = "0.71.1", "fatedier/frp"
					ext := ".tar.gz"
					if runtime.GOOS == "windows" {
						ext = ".zip"
					}
					asset = "frp_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH + ext
				}
				tag := version
				if spec.Provider == "frp" {
					tag = "v" + tag
				}
				base := "https://github.com/" + repo + "/releases/download/" + tag + "/"
				assets := []map[string]string{{"name": asset, "browser_download_url": base + asset}}
				if spec.Provider == "frp" {
					assets = append(assets, map[string]string{"name": "frp_sha256_checksums.txt", "browser_download_url": base + "frp_sha256_checksums.txt"})
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": assets})
				}))
				defer server.Close()
				spec.ReleaseAPI = server.URL
			}
			target := filepath.Join(t.TempDir(), filepath.Base(spec.Target))
			spec.Target = target
			spec.Resolve = func() (string, string, error) { _, err := os.Stat(target); return target, "managed", err }
			inspect := spec.Inspect
			spec.Inspect = func(ctx context.Context, path string) (string, error) {
				version, err := inspect(ctx, path)
				if err != nil {
					out, runErr := exec.CommandContext(ctx, path, "--version").CombinedOutput()
					t.Logf("version diagnostic: err=%v output=%q", runErr, string(out))
				}
				return version, err
			}
			manager := climaint.New(spec)
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
			defer cancel()
			status, err := manager.Check(ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			if status.CandidateID == "" || status.LatestVersion == "" {
				t.Fatal("official version not discovered", status)
			}
			status, err = manager.Apply(ctx, status.CandidateID, func() (func(), error) { return func() {}, nil })
			if err != nil {
				t.Fatal(err)
			}
			if !status.Installed || status.CurrentVersion != status.LatestVersion {
				t.Fatal("temporary executable did not match official version", status)
			}
			// Exercise real executable copies, backup validation and the restore
			// transaction without needing a second version from the network.
			candidate, err := manager.Import(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = manager.Apply(ctx, candidate.CandidateID, func() (func(), error) { return func() {}, nil }); err != nil {
				t.Fatal(err)
			}
			status, err = manager.Restore(ctx, func() (func(), error) { return func() {}, nil })
			if err != nil || status.Phase != "restored" || !status.CanRestore {
				t.Fatal("official executable restoration failed", status, err)
			}
			t.Logf("verified official %s executable %s in temporary directory", spec.Provider, status.CurrentVersion)
		})
	}
}
