// Mirrors devtunnel.NormalizeTunnelName in internal/devtunnel/name.go:
// 3-49 lowercase letters, digits, or hyphens, starting and ending with a
// letter or digit. Consecutive inner hyphens follow the current Microsoft
// public contract (tunnel_constraints.rs).
export const DEV_TUNNEL_NAME_PATTERN = /^[a-z0-9][a-z0-9-]{1,47}[a-z0-9]$/

export function normalizeDevTunnelName(raw) {
  return String(raw || '').trim().toLowerCase()
}

export function isValidDevTunnelName(raw) {
  return DEV_TUNNEL_NAME_PATTERN.test(normalizeDevTunnelName(raw))
}

// Stable marker returned by the backend (devtunnel.ErrTunnelNameConflict) so
// the UI can show a localized explanation instead of the raw CLI text.
export const DEV_TUNNEL_NAME_CONFLICT_MARKER = 'dev tunnel name is already in use'

export function isDevTunnelNameConflict(text) {
  return String(text || '').toLowerCase().includes(DEV_TUNNEL_NAME_CONFLICT_MARKER)
}
