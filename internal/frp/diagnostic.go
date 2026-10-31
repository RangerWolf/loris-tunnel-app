package frp

import (
	"strings"
	"sync"
)

// Keep only known failure categories; never persist or log raw client output.
type failureHint struct {
	mu            sync.Mutex
	pending, hint string
}

func (f *failureHint) Write(data []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	text := f.pending + strings.ToLower(string(data))
	switch {
	case strings.Contains(text, "token in login doesn't match"), strings.Contains(text, "authorization failed"), strings.Contains(text, "authentication failed"):
		f.hint = "FRP authentication failed; check the server token"
	case strings.Contains(text, "connection refused"), strings.Contains(text, "i/o timeout"), strings.Contains(text, "no such host"):
		if f.hint == "" {
			f.hint = "FRP server is unreachable; check its address, port and firewall"
		}
	}
	if len(text) > 256 {
		text = text[len(text)-256:]
	}
	f.pending = text
	return len(data), nil
}
func (f *failureHint) message() string { f.mu.Lock(); defer f.mu.Unlock(); return f.hint }
