package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath_DevModeUsesWorkingDir(t *testing.T) {
	cwd := t.TempDir()
	setWorkingDir(t, cwd)
	t.Setenv("devserver", "1")

	path := ResolveConfigPath()
	want := filepath.Join(cwd, defaultConfigPath)
	if canonicalPath(path) != canonicalPath(want) {
		t.Fatalf("ResolveConfigPath() = %q, want %q", path, want)
	}
}

func TestResolveConfigPath_BuildModeUsesHomeConfig(t *testing.T) {
	home := t.TempDir()
	setFakeHome(t, home)
	t.Setenv("devserver", "")

	path := ResolveConfigPath()
	want := filepath.Join(home, ".loris-tunnel", defaultConfigPath)
	if canonicalPath(path) != canonicalPath(want) {
		t.Fatalf("ResolveConfigPath() = %q, want %q", path, want)
	}
}

func TestMigrateFromLocalConfigIfNeeded_MigratesAndBacksUp(t *testing.T) {
	home := t.TempDir()
	setFakeHome(t, home)

	cwd := t.TempDir()
	setWorkingDir(t, cwd)

	localPath := filepath.Join(cwd, defaultConfigPath)
	localContent := []byte("version = 1\n")
	if err := os.WriteFile(localPath, localContent, 0o644); err != nil {
		t.Fatalf("write local config failed: %v", err)
	}

	targetPath := GetHomeConfigPath()
	if err := MigrateFromLocalConfigIfNeeded(targetPath); err != nil {
		t.Fatalf("MigrateFromLocalConfigIfNeeded() error = %v", err)
	}

	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read migrated config failed: %v", err)
	}
	if string(targetData) != string(localContent) {
		t.Fatalf("migrated config mismatch: got %q, want %q", targetData, localContent)
	}

	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("local config should be renamed, stat err = %v", err)
	}

	backupPath := localPath + ".old"
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup config failed: %v", err)
	}
	if string(backupData) != string(localContent) {
		t.Fatalf("backup config mismatch: got %q, want %q", backupData, localContent)
	}
}

func TestMigrateFromLocalConfigIfNeeded_NoopWhenTargetAlreadyExists(t *testing.T) {
	home := t.TempDir()
	setFakeHome(t, home)

	cwd := t.TempDir()
	setWorkingDir(t, cwd)

	localPath := filepath.Join(cwd, defaultConfigPath)
	localContent := []byte("version = 1\n")
	if err := os.WriteFile(localPath, localContent, 0o644); err != nil {
		t.Fatalf("write local config failed: %v", err)
	}

	targetPath := GetHomeConfigPath()
	targetContent := []byte("version = 2\n")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("mkdir target dir failed: %v", err)
	}
	if err := os.WriteFile(targetPath, targetContent, 0o644); err != nil {
		t.Fatalf("write target config failed: %v", err)
	}

	if err := MigrateFromLocalConfigIfNeeded(targetPath); err != nil {
		t.Fatalf("MigrateFromLocalConfigIfNeeded() error = %v", err)
	}

	gotTarget, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target config failed: %v", err)
	}
	if string(gotTarget) != string(targetContent) {
		t.Fatalf("target config should stay unchanged: got %q, want %q", gotTarget, targetContent)
	}

	gotLocal, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local config failed: %v", err)
	}
	if string(gotLocal) != string(localContent) {
		t.Fatalf("local config should stay unchanged: got %q, want %q", gotLocal, localContent)
	}

	if _, err := os.Stat(localPath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("local backup should not exist, stat err = %v", err)
	}
}

func TestMigrateFromLocalConfigIfNeeded_NoopForNonHomeTarget(t *testing.T) {
	home := t.TempDir()
	setFakeHome(t, home)

	cwd := t.TempDir()
	setWorkingDir(t, cwd)

	localPath := filepath.Join(cwd, defaultConfigPath)
	localContent := []byte("version = 1\n")
	if err := os.WriteFile(localPath, localContent, 0o644); err != nil {
		t.Fatalf("write local config failed: %v", err)
	}

	nonHomeTarget := filepath.Join(cwd, "other.toml")
	if err := MigrateFromLocalConfigIfNeeded(nonHomeTarget); err != nil {
		t.Fatalf("MigrateFromLocalConfigIfNeeded() error = %v", err)
	}

	if _, err := os.Stat(nonHomeTarget); !os.IsNotExist(err) {
		t.Fatalf("non-home target should not be created, stat err = %v", err)
	}
	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("local config should stay in place, stat err = %v", err)
	}
	if _, err := os.Stat(localPath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("local backup should not exist, stat err = %v", err)
	}
}

func setWorkingDir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %q failed: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
}

func setFakeHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
}

func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	dir := filepath.Dir(abs)
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(filepath.Join(realDir, filepath.Base(abs)))
}
