package conf

import (
	"errors"
	"fmt"
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

func NewDefaultStorage() (*Storage, error) {
	return NewStorage(ResolveConfigPath())
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

	cfg, err := parseConfigTOML(data)
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

func parseConfigTOML(data []byte) (*Config, error) {
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
