export function normalizeHostname(raw) {
  return String(raw || '')
    .trim()
    .toLowerCase()
    .replace(/^https:\/\//, '')
    .replace(/^http:\/\//, '')
    .replace(/\/+$/, '')
}

export function isValidHostname(raw) {
  const host = normalizeHostname(raw)
  if (!host || host.length > 253 || host.includes('/') || host.includes(':') || /\s/.test(host)) return false
  if (!host.includes('.')) return false
  return host.split('.').every((label) => label.length > 0 && label.length <= 63 && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label))
}

export function joinHostname(label, zone) {
  const left = normalizeHostname(label).replace(/\.+$/, '')
  const right = normalizeHostname(zone)
  if (!right) return left
  if (!left) return right
  return `${left}.${right}`
}

export function splitHostname(hostname, zones) {
  const host = normalizeHostname(hostname)
  const names = (zones || []).map((zone) => normalizeHostname(zone?.name)).filter(Boolean).sort((a, b) => b.length - a.length)
  const match = names.find((name) => host === name || host.endsWith(`.${name}`))
  if (!match) return { label: host, zone: names[0] || '' }
  if (host === match) return { label: '', zone: match }
  return { label: host.slice(0, -match.length - 1), zone: match }
}

export function cloudflarePublicURL(raw) {
  const host = normalizeHostname(raw)
  if (!isValidHostname(host)) return ''
  return `https://${host}`
}

export function shortTunnelID(id) {
  const value = String(id || '')
  if (value.length <= 12) return value
  return `${value.slice(0, 4)}…${value.slice(-3)}`
}

export function cloudflareOptionDisabled(item, ownerName) {
  return Boolean(ownerName) || item?.credentialReady === false
}
