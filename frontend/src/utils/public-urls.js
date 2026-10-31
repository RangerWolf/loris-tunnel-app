export function uniquePublicURLs(values) {
  const seen = new Set()
  return (Array.isArray(values) ? values : String(values || '').split(','))
    .map(value => String(value).trim())
    .filter(value => {
      try {
        const url = new URL(value)
        if (!['http:', 'https:'].includes(url.protocol) || seen.has(url.href)) return false
        seen.add(url.href)
        return true
      } catch {
        return false
      }
    })
}

export function devTunnelPortURLs(values, port) {
  const number = Number(port)
  if (!Number.isInteger(number) || number < 1 || number > 65535) return []
  return uniquePublicURLs(values).filter(value => {
    const url = new URL(value)
    const explicitPort = value.match(/^[a-z]+:\/\/[^/?#]+:(\d+)(?:[/?#]|$)/i)?.[1]
    return Number(url.port || explicitPort) === number || url.hostname.split('.')[0].endsWith(`-${number}`)
  })
}
