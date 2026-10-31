package devtunnel

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// ParseVersion reads the version line printed by `devtunnel --version`.
func ParseVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		const prefix = "Tunnel CLI version:"
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// ParseAccount classifies `devtunnel user show -j`.
// A failed status query stays "unknown" so a network error is not shown as signed out.
// The installed CLI returns exit code 0 with {"status":"Login token expired"}.
func ParseAccount(output string, runErr error) (state, label, detail string) {
	body := strings.TrimSpace(output)
	if body == "" {
		if runErr != nil {
			return "unknown", "", strings.TrimSpace(runErr.Error())
		}
		return "unknown", "", ""
	}
	var raw any
	if json.Unmarshal([]byte(extractJSON(body)), &raw) == nil {
		if object, ok := raw.(map[string]any); ok {
			status, _ := object["status"].(string)
			label = accountLabel(object)
			if strings.TrimSpace(status) != "" {
				state := accountFromStatus(status)
				if state == "unknown" && label != "" {
					state = "signed_in"
				}
				detail = ""
				if state != "signed_in" {
					detail = strings.TrimSpace(status)
				}
				return state, label, detail
			}
			if label != "" || object["provider"] != nil || object["loginProvider"] != nil {
				return "signed_in", label, ""
			}
		}
	}
	return accountFromStatus(body), "", clip(body, 240)
}

// ClassifyLoginLine maps one CLI login line to a UI phase.
// "not logged in" must not be treated as a successful login.
func ClassifyLoginLine(line string) (phase, url string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "not logged") || strings.Contains(lower, "logged out") {
		return "", ""
	}
	if strings.Contains(lower, "logged in") || strings.Contains(lower, "login complete") || strings.Contains(lower, "login successful") {
		return "verifying", ""
	}
	if found := firstURL(line); found != "" {
		return "browser", found
	}
	if strings.Contains(lower, "browser") {
		return "browser", ""
	}
	return "", ""
}

func firstURL(line string) string {
	for _, field := range strings.Fields(line) {
		field = strings.Trim(field, "\"'()[]<>.,")
		if strings.HasPrefix(field, "https://") || strings.HasPrefix(field, "http://") {
			return field
		}
	}
	return ""
}

func accountFromStatus(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "expired"):
		return "expired"
	case strings.Contains(lower, "not logged"), strings.Contains(lower, "logged out"), strings.Contains(lower, "signed out"):
		return "signed_out"
	case strings.Contains(lower, "logged in"), strings.Contains(lower, "signed in"):
		return "signed_in"
	default:
		return "unknown"
	}
}

func accountLabel(object map[string]any) string {
	label := firstString(object, "username", "login", "userName", "email", "name")
	provider := firstString(object, "provider", "loginProvider")
	if label == "" {
		return provider
	}
	if provider == "" {
		return label
	}
	return label + " · " + provider
}

// ParseTunnelID finds the tunnel id in CLI JSON and keeps the cluster when present.
func ParseTunnelID(output string) string {
	parsed, err := parseResourceOutput(output)
	if err == nil && parsed.ID != "" {
		return parsed.ID
	}
	payload := extractJSON(output)
	if payload == "" {
		return ""
	}
	var raw any
	if json.Unmarshal([]byte(payload), &raw) != nil {
		return ""
	}
	return findKey(raw, "tunnelId")
}

// ParseHostLine reads one host-process line.
// A browser URL is recorded only after the separate ready line is seen by the caller.
func ParseHostLine(line string) (url string, ready bool, authRequired bool) {
	line = strings.TrimSpace(line)
	lower := strings.ToLower(line)
	if strings.Contains(lower, "login token expired") || strings.Contains(lower, "not logged in") {
		return "", false, true
	}
	const browser = "Connect via browser:"
	if strings.HasPrefix(line, browser) {
		return preferBrowserURL(strings.TrimPrefix(line, browser)), false, false
	}
	if strings.HasPrefix(line, "Ready to accept connections for tunnel:") {
		return "", true, false
	}
	return "", false, false
}

// ParseHostURLs preserves all browser addresses returned by the CLI.
func ParseHostURLs(line string) []string {
	const prefix = "Connect via browser:"
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, prefix) {
		return nil
	}
	var urls []string
	for _, part := range strings.Split(strings.TrimPrefix(line, prefix), ",") {
		part = strings.Trim(strings.TrimSpace(part), "\"'")
		parsed, err := url.Parse(part)
		if err == nil && parsed.Hostname() != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") {
			urls = append(urls, part)
		}
	}
	return preferURLList(urls)
}

// preferBrowserURL keeps the HTTPS address served on port 443.
// The CLI prints both "https://<id>.<cluster>.devtunnels.ms:<port>" and
// "https://<id>-<port>.<cluster>.devtunnels.ms" on one line.
func preferBrowserURL(raw string) string {
	var fallback string
	for _, part := range strings.Split(raw, ",") {
		part = strings.Trim(strings.TrimSpace(part), "\"'")
		if !strings.HasPrefix(part, "https://") && !strings.HasPrefix(part, "http://") {
			continue
		}
		if fallback == "" {
			fallback = part
		}
		if isDefaultHTTPS(part) {
			return part
		}
	}
	return fallback
}

func isDefaultHTTPS(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return false
	}
	port := parsed.Port()
	return port == "" || port == "443"
}

func extractJSON(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return ""
	}
	return text[start : end+1]
}

func findKey(value any, key string) string {
	switch typed := value.(type) {
	case map[string]any:
		if item, ok := typed[key].(string); ok && strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
		for _, child := range typed {
			if found := findKey(child, key); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findKey(child, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// portAlreadyConfigured reports that this port is already attached to the tunnel.
// The CLI uses either "already exists" or "conflicts with an existing port".
func portAlreadyConfigured(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "already") && strings.Contains(lower, "exist") {
		return true
	}
	return strings.Contains(lower, "conflict") && strings.Contains(lower, "existing port")
}

func clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

// URLsForPort keeps only actual CLI/service URLs belonging to one published port.
func URLsForPort(urls []string, port int) []string {
	var result []string
	number := strconv.Itoa(port)
	for _, value := range urls {
		value = strings.TrimSpace(value)
		parsed, err := url.Parse(value)
		if err != nil {
			continue
		}
		label := strings.Split(parsed.Hostname(), ".")[0]
		if parsed.Port() == number || strings.HasSuffix(label, "-"+number) {
			result = append(result, value)
		}
	}
	return result
}
