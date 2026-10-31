package devtunnel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"loris-tunnel/internal/climaint"
)

func (c *Client) MaintenanceSpec() climaint.Spec {
	return climaint.Spec{Provider: "devtunnel", Target: managedBinaryPath(), Resolve: c.resolve, Unpack: materializeCLI,
		Inspect: func(ctx context.Context, path string) (string, error) {
			// .NET derives its root command from the executable name. A dot-prefixed
			// extraction name can produce an empty command alias and abort the CLI.
			if filepath.Base(path) != binaryName() {
				dir, err := os.MkdirTemp("", "loris-devtunnel-version-")
				if err != nil {
					return "", err
				}
				defer os.RemoveAll(dir)
				raw, err := os.ReadFile(path)
				if err != nil {
					return "", err
				}
				path = filepath.Join(dir, binaryName())
				if err = os.WriteFile(path, raw, 0700); err != nil {
					return "", err
				}
			}
			out, err := c.run(ctx, path, "--version")
			version := ParseVersion(out)
			if err != nil || version == "" {
				return "", fmt.Errorf("Selected executable is not a usable Dev Tunnel CLI")
			}
			return version, nil
		}}
}
func (c *Client) MaintenanceBusy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginCmd != nil || c.installing
}
