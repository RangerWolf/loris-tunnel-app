package devtunnel

import (
	"context"
	"regexp"
	"strings"
)

var diagnosticHostID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// Endpoint IDs are observations, not authenticated device identities. Never
// retain raw endpoint JSON: it can contain tokens and credential-bearing URLs.
func (c *Client) observedHostIDs(ctx context.Context, id string) []string {
	path, _, err := c.resolve()
	if err != nil {
		return nil
	}
	out, err := c.run(ctx, path, "show", id, "-j")
	if err != nil {
		return nil
	}
	return parseObservedHostIDs(out)
}

func parseObservedHostIDs(out string) []string {
	raw, err := decodeCLIJSON(out)
	if err != nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	if tunnel, ok := obj["tunnel"].(map[string]any); ok {
		obj = tunnel
	}
	endpoints, _ := obj["endpoints"].([]any)
	ids := []string{}
	seen := map[string]bool{}
	for _, endpoint := range endpoints {
		item, ok := endpoint.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["hostId"].(string)
		if diagnosticHostID.MatchString(id) && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
			if len(ids) == 4 {
				break
			}
		}
	}
	return ids
}

// Keep classification separate from the display text so omitting sensitive JSON
// cannot accidentally turn an authentication failure into an infinite retry.
type hostAttemptError struct {
	kind string
	err  error
}

func (e *hostAttemptError) Error() string { return e.err.Error() }
func (e *hostAttemptError) Unwrap() error { return e.err }

var hostDiagnosticURL = regexp.MustCompile(`https?://[^\s<>"']+`)
var hostDiagnosticToken = regexp.MustCompile(`(?i)(access[_-]?token|authorization|password|secret|token)["']?\s*[:=]\s*["']?[^\s,;]+`)

func hostDiagnosticText(text string) string {
	lines := strings.Split(redactSecrets(text), "\n")
	depth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if depth > 0 || strings.Contains(trimmed, "{") || trimmed == "[" {
			depth += strings.Count(line, "{") + strings.Count(line, "[") - strings.Count(line, "}") - strings.Count(line, "]")
			if depth < 0 {
				depth = 0
			}
			lines[i] = "[CLI structured output omitted]"
			continue
		}
		lines[i] = hostDiagnosticToken.ReplaceAllString(hostDiagnosticURL.ReplaceAllString(line, "[URL omitted]"), "$1=[redacted]")
	}
	return strings.Join(lines, "\n")
}
