function sameTunnelID(left, right) {
  const a = String(left || '').trim().toLowerCase()
  const b = String(right || '').trim().toLowerCase()
  if (!a || !b) return false
  if (a === b) return true
  const [aName, aCluster] = a.split('.')
  const [bName, bCluster] = b.split('.')
  return aName === bName && (!aCluster || !bCluster)
}

function serviceHost(value) {
  return String(value || '').trim().toLowerCase().replace(/^\[|\]$/g, '').replace(/\.$/, '')
}

export function devTunnelServiceOwner(tunnels, id, host, port, editingID) {
  if (!id || !host || !Number(port)) return null
  return tunnels.find(tunnel => Number(tunnel.id) !== Number(editingID)
    && tunnel.provider === 'devtunnel'
    && sameTunnelID(tunnel.devTunnel?.tunnelId, id)
    && serviceHost(tunnel.devTunnel?.localHost || tunnel.localHost) === serviceHost(host)
    && Number(tunnel.devTunnel?.localPort || tunnel.localPort) === Number(port)) || null
}

export function cloudflareHostnameOwner(tunnels, hostname, editingID) {
  const normalized = String(hostname || '').trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/+$/, '')
  if (!normalized) return null
  return tunnels.find(tunnel => Number(tunnel.id) !== Number(editingID)
    && tunnel.provider === 'cloudflare'
    && String(tunnel.cloudflare?.hostname || '').trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/+$/, '') === normalized) || null
}
