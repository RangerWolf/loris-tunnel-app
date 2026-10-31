package cloudflared

import (
	"encoding/json"
	"regexp"
	"strings"
)

const (
	ResourceReady       = "ready"
	ResourceAuth        = "auth"
	ResourceNetwork     = "network"
	ResourceNotFound    = "not_found"
	ResourcePermission  = "permission"
	ResourceError       = "error"
	ResourceUnavailable = "unavailable"
)

var (
	versionPattern = regexp.MustCompile(`(?i)cloudflared version[:\s]+v?([0-9][0-9A-Za-z._-]*)`)
	uuidPattern    = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	createdPattern = regexp.MustCompile(`(?i)with id (` + uuidPattern.String() + `)`)
	urlPattern     = regexp.MustCompile(`https://[^\s"'<>]+`)
)

// ParseVersion reads `cloudflared version 2026.1.0`.
func ParseVersion(output string) string {
	match := versionPattern.FindStringSubmatch(output)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

// ClassifyLoginLine reports browser or verifying when the login CLI prints progress.
func ClassifyLoginLine(line string) (phase, loginURL string) {
	lower := strings.ToLower(line)
	if url := urlPattern.FindString(line); url != "" {
		return "browser", strings.TrimRight(url, ".,)")
	}
	if strings.Contains(lower, "successfully") || strings.Contains(lower, "logged in") || strings.Contains(lower, "cert.pem") {
		return "verifying", ""
	}
	return "", ""
}

// ReadyLine matches the connection-registered logs from current cloudflared builds.
func ReadyLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "registered tunnel connection") || strings.Contains(lower, "connection registered")
}

// ParseCreatedTunnelID reads the UUID printed by `cloudflared tunnel create`.
func ParseCreatedTunnelID(output string) string {
	if match := createdPattern.FindStringSubmatch(output); len(match) > 1 && ValidTunnelID(match[1]) {
		return strings.ToLower(match[1])
	}
	if match := regexp.MustCompile(`(?i)` + uuidPattern.String() + `\.json`).FindString(output); match != "" {
		id := strings.TrimSuffix(match, ".json")
		if ValidTunnelID(id) {
			return strings.ToLower(id)
		}
	}
	return ""
}

type listedTunnel struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	CreatedAt      string          `json:"createdAt"`
	CreatedAtSnake string          `json:"created_at"`
	DeletedAt      string          `json:"deletedAt"`
	DeletedAtSnake string          `json:"deleted_at"`
	Connections    json.RawMessage `json:"connections"`
	Conns          json.RawMessage `json:"conns"`
}

// ParseTunnelList reads `cloudflared tunnel list -o json`.
// cloudflared 2026 prints `null` when the account is valid and has no tunnels.
func ParseTunnelList(output string) ([]Resource, error) {
	if strings.EqualFold(strings.TrimSpace(output), "null") {
		return []Resource{}, nil
	}
	raw := extractJSON(output)
	if raw == "" {
		return nil, errNoJSON
	}
	var items []listedTunnel
	// Decode one value: cloudflared may append diagnostic logs after the list.
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&items); err != nil {
		return nil, err
	}
	out := make([]Resource, 0, len(items))
	for _, item := range items {
		if deletedTunnel(item.DeletedAt) || deletedTunnel(item.DeletedAtSnake) {
			continue
		}
		id := strings.ToLower(strings.TrimSpace(item.ID))
		if !ValidTunnelID(id) {
			continue
		}
		created := item.CreatedAt
		if created == "" {
			created = item.CreatedAtSnake
		}
		conns := item.Connections
		if len(conns) == 0 {
			conns = item.Conns
		}
		out = append(out, Resource{
			ID:          id,
			Name:        strings.TrimSpace(item.Name),
			CreatedAt:   created,
			Connections: countConnections(conns),
		})
	}
	return out, nil
}

func countConnections(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}
	return len(items)
}

func deletedTunnel(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.HasPrefix(value, "0001-")
}

func extractJSON(output string) string {
	text := strings.TrimSpace(output)
	if text == "" {
		return ""
	}
	start := strings.IndexByte(text, '[')
	if start < 0 {
		return ""
	}
	return text[start:]
}

// classifyCLIFailure maps cloudflared stderr into the shared resource states.
func classifyCLIFailure(output string, err error) (string, string) {
	text := strings.TrimSpace(output)
	if err != nil {
		if text == "" {
			text = err.Error()
		} else {
			text += " " + err.Error()
		}
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "not logged"), strings.Contains(lower, "cert.pem"),
		strings.Contains(lower, "origin cert"), strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "authentication"), strings.Contains(lower, "please login"),
		strings.Contains(lower, "you need to login"):
		return ResourceAuth, ErrAuthRequired.Error()
	case strings.Contains(lower, "not found"), strings.Contains(lower, "does not exist"), strings.Contains(lower, "no tunnel"):
		return ResourceNotFound, ErrTunnelNotFound.Error()
	case strings.Contains(lower, "permission denied"), strings.Contains(lower, "forbidden"), strings.Contains(lower, "access denied"):
		return ResourcePermission, "Cloudflare permission was denied."
	case dnsConflictText(lower) && !dnsAlreadyOurs(lower, ""):
		return ResourceError, text
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "no such host"),
		strings.Contains(lower, "network is unreachable"), strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "tls"), strings.Contains(lower, "handshake"),
		strings.Contains(lower, "dial tcp"), strings.Contains(lower, "temporary failure"):
		return ResourceNetwork, "Cloudflare could not be reached."
	default:
		detail := clip(strings.TrimSpace(output), 300)
		if detail == "" && err != nil {
			detail = clip(err.Error(), 300)
		}
		if detail == "" {
			detail = "cloudflared command failed"
		}
		return ResourceError, detail
	}
}

// Some cloudflared credential failures exit zero. Inspect diagnostic lines only:
// tunnel names in a successful JSON list may also contain authentication words.
func accountCLIAuthFailure(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		var diagnostic struct {
			Level   string `json:"level"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal([]byte(line), &diagnostic) == nil {
			if diagnostic.Level != "error" && diagnostic.Level != "fatal" {
				continue
			}
			line = diagnostic.Message + " " + diagnostic.Error
		} else {
			lower := strings.ToLower(line)
			if !strings.HasPrefix(lower, "error ") && !strings.HasPrefix(lower, "error:") &&
				!(strings.HasPrefix(line, "20") && strings.Contains(line, " ERR ")) {
				continue
			}
		}
		state, _ := classifyCLIFailure(line, nil)
		if state == ResourceAuth || state == ResourcePermission {
			return true
		}
	}
	return false
}

func dnsConflictText(lower string) bool {
	return strings.Contains(lower, "already exists") || strings.Contains(lower, "record with that host") || strings.Contains(lower, "code: 1003")
}

func dnsAlreadyOurs(lower, id string) bool {
	if !strings.Contains(lower, "already configured") {
		return false
	}
	if strings.Contains(lower, "your tunnel") || strings.Contains(lower, "this tunnel") {
		return true
	}
	return id != "" && strings.Contains(lower, strings.ToLower(id))
}

func existingCertRefusal(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "existing certificate") && strings.Contains(lower, "overwrite")
}

func loginFailureCode(output string, err error) string {
	text := output
	if err != nil {
		text += " " + err.Error()
	}
	lower := strings.ToLower(text)
	switch {
	case existingCertRefusal(text):
		return "cloudflare_login_existing_cert"
	case strings.Contains(lower, "failed to fetch resource"),
		strings.Contains(lower, "failed to write the certificate"),
		strings.Contains(lower, "waiting for login"):
		return "cloudflare_login_incomplete"
	default:
		detail := clip(text, 240)
		if detail == "" {
			detail = "cloudflare_login_incomplete"
		}
		return detail
	}
}

func clip(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= max {
		return text
	}
	return text[:max]
}
