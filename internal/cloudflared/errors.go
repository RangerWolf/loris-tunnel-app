package cloudflared

import (
	"errors"
	"fmt"
)

var (
	ErrNotInstalled       = errors.New("cloudflared is not installed")
	ErrAuthRequired       = errors.New("Cloudflare authentication is required.")
	ErrCredentialMissing  = errors.New("Tunnel credentials are missing on this device.")
	ErrTunnelNotFound     = errors.New("Cloudflare Tunnel was not found.")
	ErrInvalidHostname    = errors.New("Public hostname is not valid.")
	ErrInvalidTunnelName  = errors.New("Tunnel name is not valid.")
	ErrAccountUnavailable = errors.New("Cloudflare account status is unavailable.")
)

type dnsConflictError struct {
	Host string
}

func (e dnsConflictError) Error() string {
	return fmt.Sprintf("A DNS record already exists for %s.", e.Host)
}

func TunnelAlreadyUsed(name string) error {
	if name == "" {
		name = "another tunnel"
	}
	return fmt.Errorf("This Tunnel ID is already used by %q.", name)
}

func HostnameAlreadyUsed(name string) error {
	if name == "" {
		name = "another tunnel"
	}
	return fmt.Errorf("Hostname is already used by %s.", name)
}

func OriginUnreachable(host string, port int) error {
	return fmt.Errorf("Cannot reach local service %s:%d.", host, port)
}
