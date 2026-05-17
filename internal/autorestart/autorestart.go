// Package autorestart spawns a delayed second instance of the current binary.
// Used after config relocation so SingleInstanceLock is released before the new process starts.
package autorestart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsWailsDevEnvironment reports whether the app is running under `wails dev`
// (devserver env is set). Auto-restart is skipped in this case.
func IsWailsDevEnvironment() bool {
	return strings.TrimSpace(os.Getenv("devserver")) != ""
}

// RelaunchDetached schedules the same executable to start after a short delay, with
// the same command-line arguments. Returns nil without scheduling when in wails dev.
func RelaunchDetached() error {
	if IsWailsDevEnvironment() {
		return nil
	}
	exe, err := resolveExecutablePath()
	if err != nil {
		return err
	}
	args := os.Args[1:]
	return scheduleRelaunch(exe, args)
}

func resolveExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	exe = filepath.Clean(exe)
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = filepath.Clean(resolved)
	}
	return exe, nil
}
