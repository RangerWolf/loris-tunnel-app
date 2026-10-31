package conf

import (
	"testing"

	"loris-tunnel/internal/model"
)

func TestOldSSHConfigOmitsProvider(t *testing.T) {
	raw := []byte(`
version = 1

[[tunnels]]
id = 1
name = "db"
mode = "local"
jumper_ids = [1]
local_host = "127.0.0.1"
local_port = 3306
remote_host = "10.0.0.8"
remote_port = 3306
status = "stopped"
`)
	cfg, err := ParseConfigTOML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnels[0].ProviderName() != model.ProviderSSH || cfg.Tunnels[0].DevTunnel != nil {
		t.Fatalf("old tunnel was not kept as ssh: %+v", cfg.Tunnels[0])
	}
	encoded := string(encodeConfigTOML(cfg))
	if containsProviderField(encoded) {
		t.Fatalf("reading an old file should not invent a provider field:\n%s", encoded)
	}
}

func TestCloneDoesNotShareDevTunnel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tunnels = []model.Tunnel{{
		ID:       1,
		Name:     "web",
		Provider: model.ProviderDevTunnel,
		DevTunnel: &model.DevTunnelConfig{
			TunnelID: "abc",
			Access:   "private",
		},
		JumperIDs: []int{2},
	}}
	cloned := cfg.Clone()
	cloned.Tunnels[0].DevTunnel.TunnelID = "changed"
	cloned.Tunnels[0].JumperIDs[0] = 9
	if cfg.Tunnels[0].DevTunnel.TunnelID != "abc" || cfg.Tunnels[0].JumperIDs[0] != 2 {
		t.Fatalf("clone shared nested data: %+v", cfg.Tunnels[0])
	}
}

func TestCloneDoesNotShareCloudflare(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tunnels = []model.Tunnel{{
		ID:       1,
		Name:     "web",
		Provider: model.ProviderCloudflare,
		Cloudflare: &model.CloudflareTunnelConfig{
			TunnelID: "03e12345-6789-4abc-8def-0123456789ab",
			Hostname: "devspace.example.com",
		},
	}}
	cloned := cfg.Clone()
	cloned.Tunnels[0].Cloudflare.TunnelID = "changed"
	if cfg.Tunnels[0].Cloudflare.TunnelID != "03e12345-6789-4abc-8def-0123456789ab" {
		t.Fatalf("clone shared cloudflare config: %+v", cfg.Tunnels[0].Cloudflare)
	}
}

func TestOldConfigLeavesCloudflareNil(t *testing.T) {
	raw := []byte(`
version = 1

[[tunnels]]
id = 2
name = "web"
provider = "devtunnel"
mode = "http"
local_host = "127.0.0.1"
local_port = 3000
status = "stopped"
`)
	cfg, err := ParseConfigTOML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnels[0].Cloudflare != nil {
		t.Fatalf("missing cloudflare field should stay nil: %+v", cfg.Tunnels[0].Cloudflare)
	}
}

func containsProviderField(encoded string) bool {
	for _, line := range splitLines(encoded) {
		if line == `provider = "ssh"` || line == "provider = 'ssh'" {
			return true
		}
	}
	return false
}

func splitLines(text string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}
