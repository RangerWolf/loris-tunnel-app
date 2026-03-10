package conf

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

var (
	ErrInvalidTOMLConfigurationFile = errors.New("invalid config TOML")
)

type Storage struct {
	path string
	mu   sync.Mutex
}

func (r *Storage) Path() string {
	return r.path
}

func NewStorage(path string) (*Storage, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("config path is empty")
	}

	return &Storage{path: trimmed}, nil
}

// MigrateFromLocalConfigIfNeeded runs a one-time migration for users who previously
// used config.toml from the current directory. When the target config path is
// ~/.loris-tunnel/config.toml and that file does not exist, but ./config.toml exists
// in the process working directory, it copies the local file to the home path and
// renames the local file to config.toml.old. No-op if any condition is not met.
func MigrateFromLocalConfigIfNeeded(targetPath string) error {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return nil
	}

	homePath := GetHomeConfigPath()
	if homePath == "" {
		return nil
	}

	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return nil
	}
	homeAbs, err := filepath.Abs(homePath)
	if err != nil {
		return nil
	}
	if filepath.Clean(targetAbs) != filepath.Clean(homeAbs) {
		return nil
	}

	if _, err := os.Stat(targetPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check target config exists: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	localPath := filepath.Join(cwd, "config.toml")
	if _, err := os.Stat(localPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("check local config exists: %w", err)
	}
	if localAbs, err := filepath.Abs(localPath); err == nil && filepath.Clean(localAbs) == filepath.Clean(targetAbs) {
		return nil
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}

	dir := filepath.Dir(targetPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config dir for migration: %w", err)
		}
	}
	if err := os.WriteFile(targetPath, data, 0o644); err != nil {
		return fmt.Errorf("write migrated config: %w", err)
	}

	oldPath := localPath + ".old"
	if err := os.Rename(localPath, oldPath); err != nil {
		return fmt.Errorf("rename local config to .old: %w", err)
	}

	return nil
}

func NewDefaultStorage() (*Storage, error) {
	path := ResolveConfigPath()
	if err := MigrateFromLocalConfigIfNeeded(path); err != nil {
		slog.Warn("config migration failed", "target", path, "error", err)
	}
	return NewStorage(path)
}

func (r *Storage) Load() (*Config, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, err := r.loadLocked()
	if err != nil {
		return nil, err
	}
	return cfg.Clone(), nil
}

func (r *Storage) Update(mutator func(cfg *Config) error) (*Config, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, err := r.loadLocked()
	if err != nil {
		return nil, err
	}
	if err := mutator(cfg); err != nil {
		return nil, err
	}

	cfg.Normalize()
	if err := r.saveLocked(cfg); err != nil {
		return nil, err
	}

	return cfg.Clone(), nil
}

func (r *Storage) loadLocked() (*Config, error) {
	if err := r.ensureParentDirLocked(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(r.path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}

		cfg := DefaultConfig()
		if err := r.saveLocked(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		cfg := DefaultConfig()
		if err := r.saveLocked(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	cfg, err := ParseConfigTOML(data)
	if err != nil {
		return nil, err
	}
	cfg.Normalize()
	return cfg, nil
}

func (r *Storage) saveLocked(cfg *Config) error {
	if err := r.ensureParentDirLocked(); err != nil {
		return err
	}

	cfg.Normalize()
	data := encodeConfigTOML(cfg)

	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmpPath, r.path)
}

func (r *Storage) ensureParentDirLocked() error {
	dir := filepath.Dir(r.path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// ParseConfigTOML parses a TOML configuration from raw bytes.
// Exported so callers (e.g. import validation in app.go) can validate a file
// before replacing the live config.
func ParseConfigTOML(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTOMLConfigurationFile, err)
	}
	cfg.Normalize()
	return cfg, nil
}

func encodeConfigTOML(cfg *Config) []byte {
	cfg.Normalize()
	var buf strings.Builder
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		// This should generally not happen with our Config struct
		return []byte("")
	}
	return []byte(buf.String())
}
