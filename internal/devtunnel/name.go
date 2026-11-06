package devtunnel

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrTunnelNameConflict reports that the requested tunnel name is already
// taken by another tunnel in the same account. Callers wrap it with %w so
// errors.Is keeps working, and the message text is a stable marker the UI
// can map to a localized explanation.
var ErrTunnelNameConflict = errors.New("dev tunnel name is already in use")

// tunnelNamePattern is the Microsoft Dev Tunnels name contract: 3-49
// lowercase letters, digits, or hyphens, starting and ending with a letter
// or digit. Consecutive inner hyphens are allowed by the current public
// contract. See tunnel_constraints.rs in microsoft/dev-tunnels.
var tunnelNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}[a-z0-9]$`)

// NormalizeTunnelName trims and lowercases a requested custom tunnel id and
// validates it against the service contract. An empty input is an error;
// callers treat an empty request as a random create.
func NormalizeTunnelName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", fmt.Errorf("dev tunnel name is required")
	}
	if !tunnelNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid dev tunnel name: use 3-49 lowercase letters, digits, or hyphens, starting and ending with a letter or digit")
	}
	return name, nil
}

// tunnelNameConflict reports whether CLI output from a create names a tunnel
// name conflict. Port wording is excluded so an unrelated message is not
// translated into a name conflict.
func tunnelNameConflict(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "existing port") {
		return false
	}
	if strings.Contains(lower, "already exist") {
		return true
	}
	return strings.Contains(lower, "409") && strings.Contains(lower, "conflict")
}
