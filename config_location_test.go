package main

import (
	"os"
	"path/filepath"
	"testing"

	"loris-tunnel/internal/cloudsync"
	"loris-tunnel/internal/conf"
)

func TestFRPSettingsFollowTheSelectedConfiguration(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	for path, value := range map[string]string{
		filepath.Join(src, conf.DefaultConfigFileName): "source-profile",
		filepath.Join(dst, conf.DefaultConfigFileName): "target-profile",
		filepath.Join(src, "frp-local.json"):           `{"servers":[{"token":"source-secret"}]}`,
		filepath.Join(dst, "frp-local.json"):           `{"servers":[{"token":"target-secret"}]}`,
	} {
		if err := conf.WritePrivateAtomic(path, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dst, "frp-local.json"))
		if err != nil || string(data) != want {
			t.Fatalf("FRP settings: %q, %v", data, err)
		}
	}
	if err := syncArtifactsToTargetDir(src, dst, false); err != nil {
		t.Fatal(err)
	}
	check(`{"servers":[{"token":"target-secret"}]}`)
	if err := syncArtifactsToTargetDir(src, dst, true); err != nil {
		t.Fatal(err)
	}
	check(`{"servers":[{"token":"source-secret"}]}`)
	if err := os.Remove(filepath.Join(src, "frp-local.json")); err != nil {
		t.Fatal(err)
	}
	if err := syncArtifactsToTargetDir(src, dst, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "frp-local.json")); !os.IsNotExist(err) {
		t.Fatal("stale credentials were retained", err)
	}
}

func TestRelocationKeepsCloudSettingsPairedWithTOML(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write := func(dir, name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(name, want string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q, %v", name, data, err)
		}
	}
	write(src, conf.DefaultConfigFileName, "source-toml")
	for _, name := range cloudsync.SettingsFiles() {
		write(src, name, "source-sync")
	}
	write(dst, conf.DefaultConfigFileName, "target-toml")
	for _, name := range cloudsync.SettingsFiles() {
		write(dst, name, "target-sync")
	}
	if err := syncArtifactsToTargetDir(src, dst, false); err != nil {
		t.Fatal(err)
	}
	check(conf.DefaultConfigFileName, "target-toml")
	for _, name := range cloudsync.SettingsFiles() {
		check(name, "target-sync")
	}
	if err := syncArtifactsToTargetDir(src, dst, true); err != nil {
		t.Fatal(err)
	}
	check(conf.DefaultConfigFileName, "source-toml")
	for _, name := range cloudsync.SettingsFiles() {
		check(name, "source-sync")
	}
	for _, name := range cloudsync.SettingsFiles() {
		if err := os.Remove(filepath.Join(src, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syncArtifactsToTargetDir(src, dst, true); err != nil {
		t.Fatal(err)
	}
	for _, name := range cloudsync.SettingsFiles() {
		if _, err := os.Stat(filepath.Join(dst, name)); !os.IsNotExist(err) {
			t.Fatal("stale target connection retained", err)
		}
	}
}
