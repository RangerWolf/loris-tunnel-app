import { ResolveFRPCImportServer, CreateTunnel, DeleteFRPServer } from '../../wailsjs/go/main/App'

export function frpImportSignature(tunnel) {
  return JSON.stringify([tunnel.frp?.serverId, tunnel.mode, tunnel.localHost, Number(tunnel.localPort),
    tunnel.frp?.proxyName, Number(tunnel.frp?.remotePort), String(tunnel.frp?.domain || '').toLowerCase()])
}

export async function importFRPC({ content, format, existingID, name, rows, existingTunnels }, api = { ResolveFRPCImportServer, CreateTunnel, DeleteFRPServer }) {
  const selected = rows.filter(row => row.selected && !row.errors.length && !row.imported)
  const result = { imported: 0, skipped: 0, failed: 0, cleanupFailed: false }
  if (!selected.length) return result
  const signatures = new Set(existingTunnels.filter(t => t.provider === 'frp').map(frpImportSignature))
  const payloadFor = (row, serverId) => ({
    name: row.displayName.trim(), provider: 'frp', mode: row.type, groupId: 0, jumperIds: [],
    localHost: row.localIP, localPort: row.localPort, remoteHost: '', remotePort: 0,
    autoStart: false, status: 'stopped', description: 'Imported from FRPC configuration',
    frp: { serverId, proxyName: row.name, remotePort: row.remotePort, domain: row.domain },
  })
  const pending = selected.filter(row => {
    if (!existingID || !signatures.has(frpImportSignature(payloadFor(row, existingID)))) return true
    row.imported = true
    row.selected = false
    result.skipped++
    return false
  })
  if (!pending.length) return result
  const server = await api.ResolveFRPCImportServer(content, format, existingID, name)
  for (const row of pending) {
    const payload = payloadFor(row, server.id)
    try {
      const signature = frpImportSignature(payload)
      if (signatures.has(signature)) { result.skipped++ } else {
        await api.CreateTunnel(payload)
        signatures.add(signature)
        result.imported++
      }
      row.imported = true
      row.selected = false
      row.saveError = false
    } catch (err) {
      row.saveError = String(err?.message || err)
      result.failed++
    }
  }
  if (!existingID && result.imported === 0) {
    try { await api.DeleteFRPServer(server.id) } catch { result.cleanupFailed = true }
  }
  return result
}
