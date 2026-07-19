package conf

import (
	"os"
	"path/filepath"
	"strings"

	"loris-tunnel/internal/model"
)

const defaultConfigPath = "config.toml"

// DefaultConfigFileName is the config file basename inside the config directory.
const DefaultConfigFileName = defaultConfigPath

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
// 2. User config directory (~/.loris-tunnel/) - for installed apps
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
	Version int                 `toml:"version"`
	Jumpers []model.Jumper      `toml:"jumpers"`
	Groups  []model.TunnelGroup `toml:"groups"`
	Tunnels []model.Tunnel      `toml:"tunnels"`
	AutoRun                 bool `toml:"auto_run"`
	TrafficMonitorEnabled   bool `toml:"traffic_monitor_enabled"`
	License                 LicenseConfig       `toml:"license"`
}

type LicenseConfig struct {
	Code string `toml:"code"`
}

// GetHomeConfigPath returns the absolute path for the home config file
// (~/.loris-tunnel/config.toml). Empty string if UserHomeDir fails.
func GetHomeConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return ""
	}
	return filepath.Join(homeDir, ".loris-tunnel", defaultConfigPath)
}

// ResolveConfigPath returns the effective config file path: implicit location
// from runtime mode, then config.root redirection when valid.
func ResolveConfigPath() string {
	return ResolveEffectiveConfigPath(ResolveImplicitConfigPath())
}

// DefaultConfig creates an empty config.
func DefaultConfig() *Config {
	return &Config{
		Version: currentConfigVersion,
		Jumpers: []model.Jumper{},
		Groups:  []model.TunnelGroup{},
		Tunnels: []model.Tunnel{},
		AutoRun:               false,
		TrafficMonitorEnabled: true,
		License:               LicenseConfig{},
	}
}

// Clone returns a detached copy.
func (c *Config) Clone() *Config {
	if c == nil {
		return DefaultConfig()
	}

	out := &Config{
		Version:               c.Version,
		AutoRun:               c.AutoRun,
		TrafficMonitorEnabled: c.TrafficMonitorEnabled,
		License:               c.License,
	}
	out.Jumpers = append(out.Jumpers, c.Jumpers...)
	out.Groups = append(out.Groups, c.Groups...)
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
	if c.Groups == nil {
		c.Groups = []model.TunnelGroup{}
	}
	if c.Tunnels == nil {
		c.Tunnels = []model.Tunnel{}
	}
	c.License.Code = strings.TrimSpace(c.License.Code)
	// AutoRun defaults to false; no need to set if already present
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
