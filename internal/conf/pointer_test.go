package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEffectiveConfigPath_UsesConfigRoot(t *testing.T) {
	home := t.TempDir()
	setFakeHome(t, home)
	t.Setenv("devserver", "")

	anchor := filepath.Join(home, ".loris-tunnel")
	custom := filepath.Join(home, "custom-data")
	if err := os.MkdirAll(anchor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}

	implicit := filepath.Join(anchor, defaultConfigPath)
	if err := os.WriteFile(filepath.Join(custom, defaultConfigPath), []byte("version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pointerPath := filepath.Join(anchor, ConfigRootFileName)
	if err := os.WriteFile(pointerPath, []byte(custom+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ResolveEffectiveConfigPath(implicit)
	want := filepath.Join(custom, defaultConfigPath)
	if canonicalPath(got) != canonicalPath(want) {
		t.Fatalf("ResolveEffectiveConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveEffectiveConfigPath_BrokenPointerFallsBack(t *testing.T) {
	home := t.TempDir()
	setFakeHome(t, home)
	t.Setenv("devserver", "")

	anchor := filepath.Join(home, ".loris-tunnel")
	if err := os.MkdirAll(anchor, 0o755); err != nil {
		t.Fatal(err)
	}
	implicit := filepath.Join(anchor, defaultConfigPath)
	pointerPath := filepath.Join(anchor, ConfigRootFileName)
	if err := os.WriteFile(pointerPath, []byte("/nonexistent-path-xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ResolveEffectiveConfigPath(implicit)
	if canonicalPath(got) != canonicalPath(implicit) {
		t.Fatalf("ResolveEffectiveConfigPath() = %q, want fallback %q", got, implicit)
	}
}
