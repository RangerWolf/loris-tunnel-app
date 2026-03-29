package traytext

import (
	"embed"
	"encoding/json"
	"sync"
)

//go:embed tray.json
var trayFS embed.FS

// Strings holds systray menu / icon labels for one locale (see tray.json).
type Strings struct {
	ShowMainTitle   string `json:"showMainTitle"`
	ShowMainTooltip string `json:"showMainTooltip"`
	QuitTitle       string `json:"quitTitle"`
	QuitTooltip     string `json:"quitTooltip"`
	IconTooltip     string `json:"iconTooltip"`
	AppTitle        string `json:"appTitle"`
}

var (
	trayOnce     sync.Once
	trayByLocale map[string]Strings
)

func load() {
	data, err := trayFS.ReadFile("tray.json")
	if err != nil {
		trayByLocale = map[string]Strings{"en": fallbackEnglish()}
		return
	}
	var m map[string]Strings
	if err := json.Unmarshal(data, &m); err != nil || len(m) == 0 {
		trayByLocale = map[string]Strings{
			"en": fallbackEnglish(),
		}
		return
	}
	trayByLocale = m
}

func fallbackEnglish() Strings {
	return Strings{
		ShowMainTitle:   "Show main window",
		ShowMainTooltip: "Bring the application window to the front",
		QuitTitle:       "Quit",
		QuitTooltip:     "Quit the application",
		IconTooltip:     "Loris Tunnel",
		AppTitle:        "Loris Tunnel",
	}
}

// ForLocale returns tray strings for a vue-i18n locale tag (e.g. zh-CN). Unknown tags fall back to en.
func ForLocale(locale string) Strings {
	trayOnce.Do(load)
	if s, ok := trayByLocale[locale]; ok && s.ShowMainTitle != "" {
		return s
	}
	if s, ok := trayByLocale["en"]; ok && s.ShowMainTitle != "" {
		return s
	}
	return fallbackEnglish()
}
