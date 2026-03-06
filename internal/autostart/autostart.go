package autostart

import (
	"os"
	"runtime"

	"github.com/emersion/go-autostart"
)

const (
	appName        = "loris-tunnel"
	appDisplayName = "Loris Tunnel"
)

// isSupported returns true if the current OS supports launch-at-login.
func isSupported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func newApp() *autostart.App {
	execPath, _ := os.Executable()
	if execPath == "" {
		execPath = "loris-tunnel"
	}
	return &autostart.App{
		Name:        appName,
		DisplayName: appDisplayName,
		Exec:        []string{execPath},
	}
}

// IsEnabled returns whether launch at login is currently enabled.
// On unsupported platforms it returns false, nil.
func IsEnabled() (bool, error) {
	if !isSupported() {
		return false, nil
	}
	return newApp().IsEnabled(), nil
}

// Enable registers the app to start at user login.
// On unsupported platforms it is a no-op and returns nil.
func Enable() error {
	if !isSupported() {
		return nil
	}
	return newApp().Enable()
}

// Disable removes the app from login startup.
// On unsupported platforms it is a no-op and returns nil.
func Disable() error {
	if !isSupported() {
		return nil
	}
	return newApp().Disable()
}
