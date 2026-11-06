export function devTunnelReconnectText(info, t) {
  if (!info) return ''
  const lines = [t(info.kind === 'host_conflict' ? 'app.tunnels.reconnect.conflict' : 'app.tunnels.reconnect.disconnected')]
  if (info.kind === 'host_conflict') {
    lines.push(info.hostIds?.length
      ? t('app.tunnels.reconnect.observedHost', { ids: info.hostIds.join(', ') })
      : t('app.tunnels.reconnect.unknownHost'))
  }
  lines.push(t('app.tunnels.reconnect.progress', { attempt: info.attempt, seconds: Math.ceil(info.delaySeconds) }))
  if (info.detail) lines.push(info.detail)
  return lines.join('\n')
}
