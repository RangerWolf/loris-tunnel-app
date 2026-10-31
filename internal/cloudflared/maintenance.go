package cloudflared

import (
	"context"
	"fmt"
	"strings"

	"loris-tunnel/internal/climaint"
)

func (c *Client) MaintenanceSpec() climaint.Spec {
	return climaint.Spec{Provider: "cloudflare", Target: managedBinaryPath(), Resolve: c.resolve, Unpack: materializeCLI, DirectURL: reqURL,
		Inspect: func(ctx context.Context, path string) (string, error) {
			out, err := c.run(ctx, path, "--version")
			version := ParseVersion(out)
			if err != nil || !strings.Contains(strings.ToLower(out), "cloudflared version") || version == "" {
				return "", fmt.Errorf("Selected executable is not a usable cloudflared")
			}
			return version, nil
		}}
}
func (c *Client) MaintenanceBusy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginActive || c.installing
}
