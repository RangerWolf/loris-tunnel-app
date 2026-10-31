package climaint

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"loris-tunnel/internal/conf"
)

type snapshot struct {
	Version    string `json:"version"`
	Digest     string `json:"digest"`
	File       string `json:"file"`
	SourcePath string `json:"sourcePath"`
	Source     string `json:"source"`
}
type selection struct {
	Selected string    `json:"selected"`
	Previous *snapshot `json:"previous,omitempty"`
}
type journal struct {
	Before        selection `json:"before"`
	TargetExisted bool      `json:"targetExisted"`
	SavedTarget   *snapshot `json:"savedTarget,omitempty"`
}

// One desktop instance owns these files; serialize transactions with resolver reads.
var fileLocks sync.Map

func fileLock(target string) *sync.Mutex {
	value, _ := fileLocks.LoadOrStore(target, &sync.Mutex{})
	return value.(*sync.Mutex)
}
func directory(target string) string   { return target + ".versions" }
func statePath(target string) string   { return filepath.Join(directory(target), "state.json") }
func journalPath(target string) string { return filepath.Join(directory(target), "transaction.json") }
func readJSON(path string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, value) != nil {
		return fmt.Errorf("CLI maintenance metadata is invalid")
	}
	return nil
}
func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return conf.WritePrivateAtomic(path, raw)
}
func loadSelection(target string) (selection, error) {
	var state selection
	err := readJSON(statePath(target), &state)
	if os.IsNotExist(err) {
		return state, nil
	}
	return state, err
}
func digest(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }
func readBinary(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxBytes {
		return nil, fmt.Errorf("CLI executable is empty, not a file, or too large")
	}
	return os.ReadFile(path)
}
func writeExecutable(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := conf.WritePrivateAtomic(path, raw); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}
func saveSnapshot(target, source, version, kind string) (*snapshot, error) {
	raw, err := readBinary(source)
	if err != nil {
		return nil, err
	}
	hash := digest(raw)
	name := "saved-" + hash + filepath.Ext(target)
	if err := writeExecutable(filepath.Join(directory(target), name), raw); err != nil {
		return nil, err
	}
	return &snapshot{Version: version, Digest: hash, File: name, SourcePath: source, Source: kind}, nil
}
func snapshotBytes(target string, saved *snapshot) ([]byte, error) {
	if saved == nil || saved.File == "" || filepath.Base(saved.File) != saved.File {
		return nil, fmt.Errorf("Previous CLI backup is missing")
	}
	raw, err := readBinary(filepath.Join(directory(target), saved.File))
	if err != nil || digest(raw) != saved.Digest {
		return nil, fmt.Errorf("Previous CLI backup is missing or damaged")
	}
	return raw, nil
}
func rollback(target string, entry journal) error {
	if entry.TargetExisted {
		raw, err := snapshotBytes(target, entry.SavedTarget)
		if err != nil {
			return err
		}
		if err := writeExecutable(target, raw); err != nil {
			return err
		}
	} else if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := writeJSON(statePath(target), entry.Before); err != nil {
		return err
	}
	if err := os.Remove(journalPath(target)); err != nil {
		return err
	}
	collectBackups(target, entry.Before.Previous)
	return nil
}
func recoverLocked(target string) error {
	var entry journal
	if err := readJSON(journalPath(target), &entry); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := rollback(target, entry); err != nil {
		return fmt.Errorf("CLI recovery failed; backups have been preserved: %w", err)
	}
	return nil
}

// SelectedPath also recovers interrupted replacements before a CLI can be started.
func SelectedPath(target string) (string, bool, error) {
	lock := fileLock(target)
	lock.Lock()
	defer lock.Unlock()
	if err := recoverLocked(target); err != nil {
		return "", false, err
	}
	state, err := loadSelection(target)
	if err != nil {
		return "", false, err
	}
	if state.Selected == "" {
		return "", false, nil
	}
	return state.Selected, true, nil
}
func collectBackups(target string, keep *snapshot) {
	files, _ := filepath.Glob(filepath.Join(directory(target), "saved-*"))
	for _, path := range files {
		if keep == nil || filepath.Base(path) != keep.File {
			_ = os.Remove(path)
		}
	}
}
