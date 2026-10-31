package frp

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Server is local-only: credentials never enter the normal config or cloud sync.
type Server struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Token      string `json:"token,omitempty"`
	HasToken   bool   `json:"hasToken"`
	ClearToken bool   `json:"clearToken,omitempty"`
	User       string `json:"user"`
	PublicHost string `json:"publicHost"`
	HTTPPort   int    `json:"httpPort"`
	TLS        bool   `json:"tls"`
}

func validHost(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func (s *Server) Normalize() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Host = strings.TrimSpace(s.Host)
	s.PublicHost = strings.TrimSpace(s.PublicHost)
	s.User = strings.TrimSpace(s.User)
	if s.Port == 0 {
		s.Port = 7000
	}
	if s.HTTPPort == 0 {
		s.HTTPPort = 80
	}
	if s.Name == "" || !validHost(s.Host) || s.Port < 1 || s.Port > 65535 || s.HTTPPort < 1 || s.HTTPPort > 65535 {
		return fmt.Errorf("invalid FRP server name, host or port")
	}
	if s.PublicHost != "" && !validHost(s.PublicHost) {
		return fmt.Errorf("invalid FRP public host")
	}
	return nil
}

func ValidateProxy(kind, host string, port, remote int, domain string) error {
	if !validHost(host) || port < 1 || port > 65535 {
		return fmt.Errorf("invalid FRP local service address")
	}
	switch kind {
	case "tcp":
		if remote < 1 || remote > 65535 {
			return fmt.Errorf("FRP remote port must be between 1 and 65535")
		}
	case "http":
		if !validHost(domain) || net.ParseIP(domain) != nil {
			return fmt.Errorf("FRP HTTP requires a domain without scheme, port or path")
		}
	default:
		return fmt.Errorf("FRP supports TCP and HTTP only")
	}
	return nil
}

func DomainName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		value = strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "http://")
	}
	if i := strings.IndexAny(value, "/?#"); i >= 0 {
		value = value[:i]
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.Trim(value, "[]")
}

func Address(s Server, kind string, remote int, domain string) string {
	host := s.PublicHost
	if host == "" {
		host = s.Host
	}
	if kind == "http" {
		domain = DomainName(domain)
		if s.HTTPPort != 80 {
			domain = net.JoinHostPort(domain, strconv.Itoa(s.HTTPPort))
		}
		return "http://" + domain
	}
	return net.JoinHostPort(host, strconv.Itoa(remote))
}

func configuration(s Server, name, kind, host string, port, remote int, domain string, adminPort int, password string) ([]byte, error) {
	if err := s.Normalize(); err != nil {
		return nil, err
	}
	if err := ValidateProxy(kind, host, port, remote, domain); err != nil {
		return nil, err
	}
	proxy := map[string]any{"name": name, "type": kind, "localIP": host, "localPort": port}
	if kind == "tcp" {
		proxy["remotePort"] = remote
	} else {
		proxy["customDomains"] = []string{domain}
	}
	config := map[string]any{
		"serverAddr": s.Host, "serverPort": s.Port, "user": s.User, "loginFailExit": false,
		"auth":      map[string]any{"method": "token", "token": s.Token},
		"transport": map[string]any{"tls": map[string]any{"enable": s.TLS}},
		"webServer": map[string]any{"addr": "127.0.0.1", "port": adminPort, "user": "loris", "password": password},
		"log":       map[string]any{"level": "warn", "disablePrintColor": true},
		"proxies":   []map[string]any{proxy},
	}
	var buf bytes.Buffer
	err := toml.NewEncoder(&buf).Encode(config)
	return buf.Bytes(), err
}
