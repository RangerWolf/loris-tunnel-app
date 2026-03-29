package uilocale

import (
	"os"
	"path/filepath"
	"strings"
)

// FileName is stored next to config.toml so the Go tray can follow the UI language.
const FileName = "ui.locale"

// Normalize maps arbitrary input to one of the supported vue-i18n locale tags.
func Normalize(raw string) string {
	s := strings.TrimSpace(raw)
	switch s {
	case "en", "zh-CN", "zh-TW", "zh-HK", "ru":
		return s
	}
	low := strings.ToLower(s)
	low = strings.ReplaceAll(low, "_", "-")
	switch {
	case strings.Contains(low, "-hk") || strings.Contains(low, "hongkong"):
		return "zh-HK"
	case strings.Contains(low, "-tw") || strings.Contains(low, "hant"):
		return "zh-TW"
	case strings.HasPrefix(low, "zh"):
		return "zh-CN"
	case strings.HasPrefix(low, "ru"):
		return "ru"
	case strings.HasPrefix(low, "en"):
		return "en"
	default:
		return "en"
	}
}

// DetectFromEnv approximates frontend/src/i18n.js detectSystemLocale using OS env.
func DetectFromEnv() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			continue
		}
		base := strings.Split(v, ".")[0]
		base = strings.ReplaceAll(base, "_", "-")
		return Normalize(base)
	}
	return "en"
}

// ReadFile returns the raw contents of ui.locale, or "" if missing.
func ReadFile(configDir string) string {
	if strings.TrimSpace(configDir) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(configDir, FileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WriteFile persists the locale tag for the next process / tray refresh.
func WriteFile(configDir, locale string) error {
	dir := strings.TrimSpace(configDir)
	if dir == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tag := Normalize(locale)
	return os.WriteFile(filepath.Join(dir, FileName), []byte(tag+"\n"), 0o644)
}

// Resolve picks saved preference, otherwise OS locale.
func Resolve(configDir string) string {
	if s := ReadFile(configDir); s != "" {
		return Normalize(s)
	}
	return DetectFromEnv()
}
