package cloudflared

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"loris-tunnel/internal/conf"
)

// RuntimeSpec is the ingress file passed to one cloudflared process.
type RuntimeSpec struct {
	TunnelID        string
	CredentialsFile string
	Hostname        string
	Protocol        string
	LocalHost       string
	LocalPort       int
}

type runtimeFile struct {
	Tunnel          string        `yaml:"tunnel"`
	CredentialsFile string        `yaml:"credentials-file"`
	Ingress         []ingressRule `yaml:"ingress"`
}

type ingressRule struct {
	Hostname string `yaml:"hostname,omitempty"`
	Service  string `yaml:"service"`
}

// WriteRuntimeConfig stores one tunnel's ingress under the user cache directory.
// It never writes ~/.cloudflared/config.yml.
func WriteRuntimeConfig(localID int, spec RuntimeSpec) (string, error) {
	if localID <= 0 {
		return "", fmt.Errorf("invalid tunnel id")
	}
	if !ValidTunnelID(spec.TunnelID) {
		return "", fmt.Errorf("invalid cloudflare tunnel id")
	}
	host, err := NormalizeHostname(spec.Hostname)
	if err != nil {
		return "", err
	}
	service, err := originService(spec.Protocol, spec.LocalHost, spec.LocalPort)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(spec.CredentialsFile) == "" {
		return "", ErrCredentialMissing
	}
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	body, err := yaml.Marshal(runtimeFile{
		Tunnel:          strings.ToLower(spec.TunnelID),
		CredentialsFile: spec.CredentialsFile,
		Ingress: []ingressRule{
			{Hostname: host, Service: service},
			{Service: "http_status:404"},
		},
	})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, strconv.Itoa(localID)+".yml")
	if err := conf.WritePrivateAtomic(path, body); err != nil {
		return "", err
	}
	return path, nil
}

func originService(protocol, host string, port int) (string, error) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported cloudflare protocol: %s", protocol)
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("localPort must be between 1 and 65535")
	}
	return strings.ToLower(protocol) + "://" + net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(port)), nil
}

func runtimeDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "loris-tunnel", "cloudflared")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
