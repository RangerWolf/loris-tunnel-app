export function latencyLabel(tunnel, t, format) {
  if (tunnel.status !== 'running') return '—'
  if ((tunnel.provider || 'ssh') === 'ssh') {
    return tunnel.latencyMs > 0 ? format(tunnel.latencyMs) : t('app.tunnels.latency.measuring')
  }
  if (tunnel.latencyState === 'ok' && tunnel.latencyMs > 0) return format(tunnel.latencyMs)
  const states = { timeout: 'timeout', failed: 'failed', auth_required: 'authRequired', restricted: 'restricted' }
  return t(`app.tunnels.latency.${states[tunnel.latencyState] || 'measuring'}`)
}

export function latencyTitle(tunnel, t) {
  if ((tunnel.provider || 'ssh') === 'ssh') return t('app.tunnels.latency.sshHint')
  const lines = [t(tunnel.provider === 'frp' && tunnel.mode === 'tcp' ? 'app.tunnels.latency.tcpHint' : 'app.tunnels.latency.publicHint')]
  if (tunnel.status !== 'running') return lines[0]
  if (tunnel.latencyHttpStatus) lines.push(`HTTP ${tunnel.latencyHttpStatus}`)
  if (tunnel.latencyDetail) lines.push(tunnel.latencyDetail)
  if (tunnel.latencyCheckedAt) {
    const date = new Date(tunnel.latencyCheckedAt)
    if (!Number.isNaN(date.getTime())) lines.push(`${t('app.tunnels.latency.checkedAt')}: ${date.toLocaleString()}`)
  }
  return lines.join('\n')
}

export function formatLatencyLabel(latencyMs) {
  const ms = Number(latencyMs)
  if (!Number.isFinite(ms) || ms <= 0) return '--'
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(ms < 10000 ? 2 : 1)} s`
  if (ms < 3600000) return `${(ms / 60000).toFixed(ms < 600000 ? 2 : 1)} min`
  return `${(ms / 3600000).toFixed(2)} h`
}
