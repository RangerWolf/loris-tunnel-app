package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"loris-tunnel/internal/autorestart"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/uilocale"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ConfigLocationInfo describes implicit vs effective config paths and the pointer file.
type ConfigLocationInfo struct {
	EffectiveConfigPath   string `json:"effectiveConfigPath"`
	EffectiveConfigDir    string `json:"effectiveConfigDir"`
	ImplicitConfigPath    string `json:"implicitConfigPath"`
	ImplicitConfigDir     string `json:"implicitConfigDir"`
	AnchorDir             string `json:"anchorDir"`
	ConfigRootPointerPath string `json:"configRootPointerPath"`
	IsCustomConfigDir     bool   `json:"isCustomConfigDir"`
}

// ConfigDirTargetConflict describes pre-existing artifact files under a relocation target dir.
type ConfigDirTargetConflict struct {
	HasConfigToml bool `json:"hasConfigToml"`
	HasUILocale   bool `json:"hasUILocale"`
}

// GetConfigLocationInfo returns paths for display in settings (custom dir via config.root).
func (a *App) GetConfigLocationInfo() (ConfigLocationInfo, error) {
	var out ConfigLocationInfo
	if err := a.ensureReady(); err != nil {
		return out, err
	}
	implicit := conf.ResolveImplicitConfigPath()
	effective := a.storage.Path()
	out.EffectiveConfigPath = effective
	out.EffectiveConfigDir = filepath.Dir(effective)
	out.ImplicitConfigPath = implicit
	out.ImplicitConfigDir = filepath.Dir(implicit)
	out.AnchorDir = conf.AnchorDirFromImplicit(implicit)
	out.ConfigRootPointerPath = conf.ConfigRootPointerPath(implicit)

	impAbs, e1 := filepath.Abs(implicit)
	effAbs, e2 := filepath.Abs(effective)
	if e1 != nil || e2 != nil {
		out.IsCustomConfigDir = filepath.Clean(implicit) != filepath.Clean(effective)
	} else {
		out.IsCustomConfigDir = filepath.Clean(impAbs) != filepath.Clean(effAbs)
	}
	return out, nil
}

// SelectConfigDirectory opens an OS folder picker. Returns empty string if cancelled.
func (a *App) SelectConfigDirectory() (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}
	defaultDir := ""
	if info, err := a.GetConfigLocationInfo(); err == nil {
		if st, e := os.Stat(info.EffectiveConfigDir); e == nil && st.IsDir() {
			defaultDir = info.EffectiveConfigDir
		}
	}
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Select configuration directory",
		DefaultDirectory: defaultDir,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(dir), nil
}

// GetConfigDirTargetConflict reports whether the target directory already has config.toml / ui.locale.
func (a *App) GetConfigDirTargetConflict(targetDir string) (ConfigDirTargetConflict, error) {
	var out ConfigDirTargetConflict
	if err := a.ensureReady(); err != nil {
		return out, err
	}
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return out, fmt.Errorf("target directory is empty")
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return out, fmt.Errorf("resolve target directory: %w", err)
	}
	absTarget = filepath.Clean(absTarget)
	out.HasConfigToml = regularExistsFile(filepath.Join(absTarget, conf.DefaultConfigFileName))
	out.HasUILocale = regularExistsFile(filepath.Join(absTarget, uilocale.FileName))
	return out, nil
}

// SetConfigDirectory copies (or selectively keeps) config.toml / ui.locale into targetDir — same atomic+MD5
// path for both — then writes config.root and removes prior files. overwriteExisting applies when targets
// already exist: false keeps existing files on disk and only fills gaps from the current sources.
func (a *App) SetConfigDirectory(targetDir string, overwriteExisting bool) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return fmt.Errorf("target directory is empty")
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("resolve target directory: %w", err)
	}
	absTarget = filepath.Clean(absTarget)
	if !conf.IsDirReadableAndWritable(absTarget) {
		return fmt.Errorf("target directory is not readable and writable: %s", absTarget)
	}

	implicit := conf.ResolveImplicitConfigPath()
	anchor := conf.AnchorDirFromImplicit(implicit)
	srcPath := a.storage.Path()
	srcDir := filepath.Dir(srcPath)
	dstPath := filepath.Join(absTarget, conf.DefaultConfigFileName)

	if pathsEqualPathfile(srcPath, dstPath) {
		return nil
	}

	if a.tunnel != nil {
		a.tunnel.Shutdown()
	}

	if err := syncArtifactsToTargetDir(srcDir, absTarget, overwriteExisting); err != nil {
		return err
	}

	// Commit pointer after successful copy + checksum.
	if pathsEqualPathfile(anchor, absTarget) {
		if err := conf.RemoveConfigRootPointer(anchor); err != nil {
			return fmt.Errorf("clear config.root: %w", err)
		}
	} else {
		if err := conf.WriteConfigRootPointer(anchor, absTarget); err != nil {
			return fmt.Errorf("write config.root: %w", err)
		}
	}

	// Remove previous config files (not logs).
	if !pathsEqualPathfile(srcDir, absTarget) {
		if err := conf.TryRemoveFile(srcPath); err != nil {
			slog.Warn("could not remove previous config.toml", "path", srcPath, "error", err)
		}
		_ = conf.TryRemoveFile(filepath.Join(srcDir, uilocale.FileName))
	}

	slog.Info("config directory relocated", "anchor", anchor, "target", absTarget)
	return nil
}

// ResetConfigDirectoryToDefault copies the live config back to the implicit path when using
// a custom directory, removes config.root, deletes previous custom files, then expects a restart.
// If the app already uses the implicit path, only config.root is removed if present.
// overwriteExisting mirrors SetConfigDirectory: true replaces existing artifacts in the default dir.
func (a *App) ResetConfigDirectoryToDefault(overwriteExisting bool) error {
	if err := a.ensureReady(); err != nil {
		return err
	}

	implicit := conf.ResolveImplicitConfigPath()
	anchor := conf.AnchorDirFromImplicit(implicit)
	srcPath := a.storage.Path()
	srcDir := filepath.Dir(srcPath)

	if pathsEqualPathfile(srcPath, implicit) {
		return conf.RemoveConfigRootPointer(anchor)
	}

	if a.tunnel != nil {
		a.tunnel.Shutdown()
	}

	implicitDir := filepath.Dir(implicit)
	if err := syncArtifactsToTargetDir(srcDir, implicitDir, overwriteExisting); err != nil {
		return fmt.Errorf("copy config to default location: %w", err)
	}

	if err := conf.RemoveConfigRootPointer(anchor); err != nil {
		return fmt.Errorf("remove config.root: %w", err)
	}

	_ = conf.TryRemoveFile(srcPath)
	_ = conf.TryRemoveFile(filepath.Join(srcDir, uilocale.FileName))

	slog.Info("config directory reset to default", "implicit", implicit)
	return nil
}

// QuitApplication exits the process so config on disk reloads on next startup.
// In packaged builds this schedules an automatic restart after ~2s; under wails dev it only quits.
func (a *App) QuitApplication() {
	if a.ctx == nil {
		return
	}
	a.PrepareForQuit()
	if err := autorestart.RelaunchDetached(); err != nil {
		slog.Warn("automatic relaunch failed; start the app manually if needed", "error", err)
	}
	wailsruntime.Quit(a.ctx)
}

func regularExistsFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// syncArtifactsToTargetDir aligns config.toml and ui.locale between srcDir and dstDir using the same
// AtomicCopyFileWithMD5Verify for each file copied. overwriteExisting replaces both when source files exist.
func syncArtifactsToTargetDir(srcDir, dstDir string, overwriteExisting bool) error {
	dstToml := filepath.Join(dstDir, conf.DefaultConfigFileName)
	srcToml := filepath.Join(srcDir, conf.DefaultConfigFileName)
	copyToml := overwriteExisting || !regularExistsFile(dstToml)
	if copyToml {
		if err := conf.AtomicCopyFileWithMD5Verify(srcToml, dstToml); err != nil {
			return fmt.Errorf("copy config: %w", err)
		}
	}

	dstLoc := filepath.Join(dstDir, uilocale.FileName)
	srcLoc := filepath.Join(srcDir, uilocale.FileName)
	srcHasLocale := regularExistsFile(srcLoc)

	var localeShouldCopy bool
	switch {
	case overwriteExisting:
		localeShouldCopy = srcHasLocale
	case regularExistsFile(dstLoc):
		localeShouldCopy = false
	case srcHasLocale:
		localeShouldCopy = true
	default:
		localeShouldCopy = false
	}

	if !localeShouldCopy {
		return nil
	}

	if err := conf.AtomicCopyFileWithMD5Verify(srcLoc, dstLoc); err != nil {
		if copyToml {
			_ = conf.TryRemoveFile(dstToml)
		}
		return fmt.Errorf("copy ui.locale: %w", err)
	}
	return nil
}

func pathsEqualPathfile(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" && b == "" {
		return true
	}
	aa, e1 := filepath.Abs(a)
	bb, e2 := filepath.Abs(b)
	if e1 != nil || e2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
