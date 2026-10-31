package frp

import (
	"context"
	"fmt"
	"loris-tunnel/internal/climaint"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Client struct{ Store *Store }
type Status struct {
	Installed bool     `json:"installed"`
	Version   string   `json:"version"`
	Path      string   `json:"path"`
	Detail    string   `json:"detail"`
	Servers   []Server `json:"servers"`
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "frpc.exe"
	}
	return "frpc"
}
func (c *Client) managedPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Dir(c.Store.Path)
	}
	return filepath.Join(dir, "LorisTunnel", "frp", executableName())
}
func (c *Client) resolve() (string, error) {
	cfg, err := c.Store.Settings()
	if err != nil {
		return "", err
	}
	if selected, chosen, err := climaint.SelectedPath(c.managedPath()); err != nil {
		return "", err
	} else if chosen {
		return selected, nil
	}
	if cfg.Binary != "" {
		return cfg.Binary, nil
	}
	if _, err := os.Stat(c.managedPath()); err == nil {
		return c.managedPath(), nil
	}
	return exec.LookPath(executableName())
}
func inspectBinary(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	configureCommand(cmd)
	data, err := cmd.Output()
	version := strings.TrimSpace(string(data))
	if err != nil || len(version) > 100 || len(version) == 0 || version[0] < '0' || version[0] > '9' {
		return "", fmt.Errorf("cannot execute frpc --version; choose an official frpc binary")
	}
	return version, nil
}
func (c *Client) Inspect(ctx context.Context) (Status, error) {
	cfg, err := c.Store.Settings()
	s := Status{Servers: cfg.Servers}
	if err != nil {
		return s, err
	}
	path, err := c.resolve()
	if err != nil {
		s.Detail = "frpc is not installed"
		return s, nil
	}
	s.Path = path
	s.Version, err = inspectBinary(ctx, path)
	if err != nil {
		s.Detail = err.Error()
		return s, nil
	}
	s.Installed = true
	return s, nil
}
func (c *Client) SelectBinary(ctx context.Context, path string) error {
	if _, err := inspectBinary(ctx, path); err != nil {
		return err
	}
	return c.Store.SetBinary(path)
}
