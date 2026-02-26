package conf

import (
	"os"
	"path/filepath"
	"strings"

	"loris-tunnel/internal/model"
)

const defaultConfigPath = "config.toml"
const currentConfigVersion = 1

// isDirWritable checks if a directory is writable by attempting to create a temp file.
func isDirWritable(dir string) bool {
	tmpFile, err := os.CreateTemp(dir, ".write_test_*")
	if err != nil {
		return false
	}
	tmpFile.Close()
	os.Remove(tmpFile.Name())
	return true
}

// getDefaultConfigDir returns the default config directory for the app.
// Priority:
// 1. Current directory (if writable) - for development and portable mode
// 2. User config directory (~/.config/loris-tunnel/) - for installed apps
func getDefaultConfigDir() string {
	// Try current directory first (for development and portable mode)
	cwd, err := os.Getwd()
	if err == nil && isDirWritable(cwd) {
		return cwd
	}

	// Fallback to user config directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Last resort: current directory even if not writable
		return "."
	}
	return filepath.Join(homeDir, ".loris-tunnel")
}

// Config is persisted in TOML storage.
type Config struct {
	Version int            `toml:"version"`
	Jumpers []model.Jumper `toml:"jumpers"`
	Tunnels []model.Tunnel `toml:"tunnels"`
}

// ResolveConfigPath returns config path from env or default path.
// On macOS, defaults to ~/.config/loris-tunnel/config.toml to avoid read-only app bundle.
func ResolveConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("LORIS_TUNNEL_CONFIG_PATH")); path != "" {
		return path
	}
	return filepath.Join(getDefaultConfigDir(), defaultConfigPath)
}

// DefaultConfig creates an empty config.
func DefaultConfig() *Config {
	return &Config{
		Version: currentConfigVersion,
		Jumpers: []model.Jumper{},
		Tunnels: []model.Tunnel{},
	}
}

// Clone returns a detached copy.
func (c *Config) Clone() *Config {
	if c == nil {
		return DefaultConfig()
	}

	out := &Config{Version: c.Version}
	out.Jumpers = append(out.Jumpers, c.Jumpers...)
	out.Tunnels = append(out.Tunnels, c.Tunnels...)
	return out
}

// Normalize ensures stable defaults before save.
func (c *Config) Normalize() {
	if c.Version <= 0 {
		c.Version = currentConfigVersion
	}
	if c.Jumpers == nil {
		c.Jumpers = []model.Jumper{}
	}
	if c.Tunnels == nil {
		c.Tunnels = []model.Tunnel{}
	}
	for i := range c.Tunnels {
		c.Tunnels[i].JumperIDs = normalizeJumperIDs(c.Tunnels[i].JumperIDs)
	}
}

func normalizeJumperIDs(ids []int) []int {
	out := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids)+1)
	appendID := func(id int) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range ids {
		appendID(id)
	}
	return out
}
