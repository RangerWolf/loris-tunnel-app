package conf

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ConfigRootFileName is the pointer file in the anchor directory; it contains
// one line: absolute path of the directory that holds config.toml.
const ConfigRootFileName = "config.root"

// ResolveImplicitConfigPath returns the config file path without applying
// config.root redirection (legacy ResolveConfigPath semantics).
func ResolveImplicitConfigPath() string {
	if strings.TrimSpace(os.Getenv("devserver")) != "" {
		return filepath.Join(getDefaultConfigDir(), defaultConfigPath)
	}

	if homePath := GetHomeConfigPath(); homePath != "" {
		return homePath
	}

	return filepath.Join(getDefaultConfigDir(), defaultConfigPath)
}

// ResolveEffectiveConfigPath applies config.root next to the implicit config.toml
// if present and valid; otherwise returns implicitPath unchanged.
func ResolveEffectiveConfigPath(implicitPath string) string {
	implicitPath = strings.TrimSpace(implicitPath)
	if implicitPath == "" {
		return ResolveImplicitConfigPath()
	}

	anchorDir := filepath.Dir(implicitPath)
	pointerPath := filepath.Join(anchorDir, ConfigRootFileName)
	data, err := os.ReadFile(pointerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return implicitPath
		}
		slog.Warn("config.root read failed, using implicit config path", "path", pointerPath, "error", err)
		return implicitPath
	}

	line := strings.TrimSpace(strings.Split(string(data), "\n")[0])
	raw := filepath.Clean(line)
	if raw == "." || raw == "" {
		return implicitPath
	}
	if !filepath.IsAbs(raw) {
		slog.Warn("config.root must contain an absolute directory path", "value", raw)
		return implicitPath
	}

	st, err := os.Stat(raw)
	if err != nil {
		slog.Warn("config.root target directory unavailable, using implicit config path", "dir", raw, "error", err)
		return implicitPath
	}
	if !st.IsDir() {
		slog.Warn("config.root target is not a directory, using implicit config path", "path", raw)
		return implicitPath
	}
	if !IsDirReadableAndWritable(raw) {
		slog.Warn("config.root target directory not readable/writable enough, using implicit config path", "dir", raw)
		return implicitPath
	}

	effective := filepath.Join(raw, defaultConfigPath)
	if implicitAbs, e1 := filepath.Abs(implicitPath); e1 == nil {
		if effAbs, e2 := filepath.Abs(effective); e2 == nil {
			if filepath.Clean(implicitAbs) == filepath.Clean(effAbs) {
				return implicitPath
			}
		}
	}

	return effective
}

// AnchorDirFromImplicit returns the parent directory of the implicit config file
// (where config.root lives).
func AnchorDirFromImplicit(implicitPath string) string {
	return filepath.Dir(strings.TrimSpace(implicitPath))
}

// ConfigRootPointerPath returns the full path to config.root for a given implicit config path.
func ConfigRootPointerPath(implicitPath string) string {
	return filepath.Join(AnchorDirFromImplicit(implicitPath), ConfigRootFileName)
}

// IsDirReadableAndWritable checks the directory is stat'd as directory, readable
// (openable) and writable (temp file test).
func IsDirReadableAndWritable(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	_ = f.Close()
	return isDirWritable(dir)
}
