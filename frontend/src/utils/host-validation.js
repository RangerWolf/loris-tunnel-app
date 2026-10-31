export function isValidTargetHost(host) {
  if (typeof host !== 'string' || !host || host !== host.trim()) return false
  if (host.includes(':')) {
    if (host.includes('[') || host.includes(']') || host.includes('%')) return false
    try {
      const address = new URL(`http://[${host}]/`).hostname
      return address.startsWith('[') && !['[::]', '[::ffff:0:0]'].includes(address)
    } catch {
      return false
    }
  }
  if (/^[\d.]+$/.test(host)) {
    const parts = host.split('.')
    return host !== '0.0.0.0' && parts.length === 4 && parts.every((part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255)
  }
  return host.length <= 253 && host.replace(/\.$/, '').split('.').every((label) =>
    label.length <= 63 && /^[a-z\d](?:[a-z\d-]*[a-z\d])?$/i.test(label))
}
