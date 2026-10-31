package frp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"loris-tunnel/internal/climaint"
)

func (c *Client) MaintenanceSpec() climaint.Spec {
	return climaint.Spec{Provider: "frp", Target: c.managedPath(), Resolve: func() (string, string, error) {
		path, err := c.resolve()
		source := "system"
		if path == c.managedPath() {
			source = "managed"
		}
		return path, source, err
	}, Unpack: func(archive, dir string) (string, error) {
		raw, err := os.ReadFile(archive)
		if err != nil {
			return "", err
		}
		binary, err := extractBinary(raw, len(raw) > 1 && string(raw[:2]) == "PK")
		if err != nil {
			return "", err
		}
		path := filepath.Join(dir, executableName())
		return path, os.WriteFile(path, binary, 0700)
	}, Inspect: func(ctx context.Context, path string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		version, err := inspectBinary(ctx, path)
		if err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, path, "--help")
		configureCommand(cmd)
		out, err := cmd.Output()
		if err != nil || !strings.Contains(strings.ToLower(string(out)), "frpc") {
			return "", fmt.Errorf("Selected executable is not a usable frpc")
		}
		return version, nil
	}}
}
