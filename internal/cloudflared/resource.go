package cloudflared

import (
	"context"
	"loris-tunnel/internal/conf"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Resource is one Cloudflare named tunnel visible to the local CLI.
type Resource struct {
	Cached          bool   `json:"cached,omitempty"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	CreatedAt       string `json:"createdAt,omitempty"`
	Connections     int    `json:"connections,omitempty"`
	CredentialReady bool   `json:"credentialReady"`
}

// ResourceList is a CLI query result. A failed query is never an empty ready list.
type ResourceList struct {
	State  string     `json:"state"`
	Detail string     `json:"detail"`
	Items  []Resource `json:"items"`
}

// ResourceDetail is one tunnel lookup.
type ResourceDetail struct {
	State    string   `json:"state"`
	Detail   string   `json:"detail"`
	Resource Resource `json:"resource"`
}

// CreateResourceInput creates a named tunnel immediately.
type CreateResourceInput struct {
	Name string `json:"name"`
}

// DeleteResourceResult reports whether the remote tunnel was removed.
type DeleteResourceResult struct {
	State      string `json:"state"`
	Detail     string `json:"detail"`
	TunnelName string `json:"tunnelName"`
}

var tunnelNameClean = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,118}[\p{L}\p{N}]$|^[\p{L}\p{N}]$`)

// ValidTunnelID reports whether id is a Cloudflare tunnel UUID.
func ValidTunnelID(id string) bool {
	return uuidPattern.MatchString(strings.TrimSpace(id))
}

// SameTunnelID compares UUID strings without case sensitivity.
func SameTunnelID(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	return a != "" && a == b
}

// ValidTunnelName rejects empty values and shell-significant characters.
// cloudflared remains the final authority.
func ValidTunnelName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "/\\:*?\"<>|\n\r\t") {
		return false
	}
	return tunnelNameClean.MatchString(name)
}

// NormalizeHostname strips a scheme and trailing slash from a public hostname.
func NormalizeHostname(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimRight(host, "/")
	if host == "" || strings.Contains(host, "/") || strings.Contains(host, "://") || strings.ContainsAny(host, " \t?#:") {
		return "", ErrInvalidHostname
	}
	if !strings.Contains(host, ".") || len(host) > 253 {
		return "", ErrInvalidHostname
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidHostname
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return "", ErrInvalidHostname
			}
		}
	}
	if net.ParseIP(host) != nil {
		return "", ErrInvalidHostname
	}
	return host, nil
}

func PublicURL(hostname string) string {
	host, err := NormalizeHostname(hostname)
	if err != nil {
		return ""
	}
	return "https://" + host
}

// ListResources reads named tunnels. It does not create or delete anything.
func (c *Client) ListResources(ctx context.Context) ResourceList {
	path, _, err := c.resolve()
	if err != nil {
		return ResourceList{State: ResourceUnavailable, Detail: ErrNotInstalled.Error()}
	}
	if !c.accountCredentialExists() {
		return ResourceList{State: ResourceAuth, Detail: ErrAuthRequired.Error()}
	}
	out, err := c.runAccount(ctx, path, "tunnel", "list", "-o", "json")
	if err != nil {
		state, detail := classifyCLIFailure(out, err)
		return ResourceList{State: state, Detail: detail}
	}
	items, parseErr := ParseTunnelList(out)
	if parseErr != nil {
		return ResourceList{State: ResourceError, Detail: "cloudflare_list_invalid_response"}
	}
	for i := range items {
		items[i].CredentialReady = c.CredentialExists(items[i].ID)
	}
	return ResourceList{State: ResourceReady, Items: items}
}

// GetResource finds one tunnel in the JSON list. Hostname is not inferred.
func (c *Client) GetResource(ctx context.Context, id string) ResourceDetail {
	id = strings.TrimSpace(id)
	if !ValidTunnelID(id) {
		return ResourceDetail{State: ResourceError, Detail: "invalid cloudflare tunnel id"}
	}
	list := c.ListResources(ctx)
	if list.State != ResourceReady {
		return ResourceDetail{State: list.State, Detail: list.Detail}
	}
	for _, item := range list.Items {
		if SameTunnelID(item.ID, id) {
			return ResourceDetail{State: ResourceReady, Resource: item}
		}
	}
	return ResourceDetail{State: ResourceNotFound, Detail: ErrTunnelNotFound.Error()}
}

// CreateResource runs `cloudflared tunnel create`.
func (c *Client) CreateResource(ctx context.Context, input CreateResourceInput) ResourceDetail {
	name := strings.TrimSpace(input.Name)
	if !ValidTunnelName(name) {
		return ResourceDetail{State: ResourceError, Detail: ErrInvalidTunnelName.Error()}
	}
	path, _, err := c.resolve()
	if err != nil {
		return ResourceDetail{State: ResourceUnavailable, Detail: ErrNotInstalled.Error()}
	}
	if !c.accountCredentialExists() {
		return ResourceDetail{State: ResourceAuth, Detail: ErrAuthRequired.Error()}
	}
	dir, dirErr := c.configDir()
	if dirErr != nil {
		return ResourceDetail{State: ResourceError, Detail: "Cloudflare credential directory is unavailable"}
	}

	temporary, tempErr := os.CreateTemp(dir, ".loris-credential-*")
	if tempErr != nil {
		return ResourceDetail{State: ResourceError, Detail: "Could not prepare tunnel credentials"}
	}
	tempPath := temporary.Name()
	_ = temporary.Close()
	_ = os.Remove(tempPath)
	defer os.Remove(tempPath)
	out, err := c.runAccount(ctx, path, "tunnel", "--credentials-file", tempPath, "create", name)
	id := ParseCreatedTunnelID(out)
	if !ValidTunnelID(id) {
		if err == nil {
			return ResourceDetail{State: ResourceError, Detail: "cloudflared did not return a tunnel id"}
		}
		state, detail := classifyCLIFailure(out, err)
		return ResourceDetail{State: state, Detail: detail}
	}

	if raw, readErr := os.ReadFile(tempPath); readErr == nil {
		if writeErr := conf.WritePrivateAtomic(filepath.Join(dir, strings.ToLower(id)+".json"), raw); writeErr != nil {
			return ResourceDetail{State: ResourceError, Detail: "Could not save tunnel credentials"}
		}
	}
	if !c.CredentialExists(id) {
		return ResourceDetail{State: ResourceError, Detail: "Cloudflare did not save tunnel credentials"}
	}
	return ResourceDetail{State: ResourceReady, Resource: Resource{
		ID:              strings.ToLower(id),
		Name:            name,
		CredentialReady: c.CredentialExists(id),
	}}
}

// DeleteRemote removes a named tunnel. It does not pass -f and does not edit DNS.
func (c *Client) DeleteRemote(ctx context.Context, id string) error {
	if !ValidTunnelID(id) {
		return ErrTunnelNotFound
	}
	path, _, err := c.resolve()
	if err != nil {
		return ErrNotInstalled
	}
	out, err := c.runAccount(ctx, path, "tunnel", "delete", id)
	if err != nil {
		_, detail := classifyCLIFailure(out, err)
		return fmtError(detail)
	}
	return nil
}

// EnsureDNSRoute creates a CNAME when it is missing.
// A record that already points at this tunnel is success. Other records are left unchanged.
func (c *Client) EnsureDNSRoute(ctx context.Context, id, hostname string) error {
	if !ValidTunnelID(id) {
		return ErrTunnelNotFound
	}
	host, err := NormalizeHostname(hostname)
	if err != nil {
		return err
	}
	path, _, err := c.resolve()
	if err != nil {
		return ErrNotInstalled
	}
	client, err := c.hostnameClient(ctx, host)
	if err != nil {
		return err
	}
	out, runErr := client.runAccount(ctx, path, "tunnel", "route", "dns", id, host)
	text := strings.ToLower(out)
	if runErr != nil {
		text += " " + strings.ToLower(runErr.Error())
	}
	if runErr == nil || dnsAlreadyOurs(text, id) {
		return nil
	}
	if dnsConflictText(text) {
		return dnsConflictError{Host: host}
	}
	_, detail := classifyCLIFailure(out, runErr)
	return fmtError(detail)
}

func fmtError(detail string) error {
	if strings.TrimSpace(detail) == "" {
		return errNoJSON
	}
	return &plainError{text: detail}
}

type plainError struct{ text string }

func (e *plainError) Error() string { return e.text }
