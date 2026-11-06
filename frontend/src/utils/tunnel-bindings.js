export function sameTunnelID(left, right) {
  const a = String(left || '').trim().toLowerCase()
  const b = String(right || '').trim().toLowerCase()
  if (!a || !b) return false
  if (a === b) return true
  const [aName, aCluster] = a.split('.')
  const [bName, bCluster] = b.split('.')
  return aName === bName && (!aCluster || !bCluster)
}

export function devTunnelServiceOwner(tunnels, id, editingID) {
  if (!id) return null
  return tunnels.find(tunnel => Number(tunnel.id) !== Number(editingID)
    && tunnel.provider === 'devtunnel'
    && sameTunnelID(tunnel.devTunnel?.tunnelId, id)) || null
}

// devTunnelNameOwner finds another local Dev Tunnel service that already
// binds the requested id or is waiting to create it. The remote create call
// remains the final authority; this only feeds an early warning.
export function devTunnelNameOwner(tunnels, name, editingID) {
  const normalized = String(name || '').trim().toLowerCase()
  if (!normalized) return null
  return tunnels.find(tunnel => Number(tunnel.id) !== Number(editingID)
    && tunnel.provider === 'devtunnel'
    && (sameTunnelID(tunnel.devTunnel?.tunnelId, normalized)
      || String(tunnel.devTunnel?.tunnelName || '').trim().toLowerCase() === normalized)) || null
}

export function cloudflareHostnameOwner(tunnels, hostname, editingID) {
  const normalized = String(hostname || '').trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/+$/, '')
  if (!normalized) return null
  return tunnels.find(tunnel => Number(tunnel.id) !== Number(editingID)
    && tunnel.provider === 'cloudflare'
    && String(tunnel.cloudflare?.hostname || '').trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/+$/, '') === normalized) || null
}
