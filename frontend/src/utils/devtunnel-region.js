export function devTunnelCluster(id) {
  const value = String(id || '').trim().toLowerCase()
  const dot = value.indexOf('.')
  if (dot < 1 || dot === value.length - 1) return ''
  return value.slice(dot + 1)
}

export function orderedRegions(regions) {
  const list = Array.isArray(regions) ? regions : []
  const ranked = list
    .filter((region) => region && region.status === 'ok' && Number.isFinite(region.latencyMs))
    .slice()
    .sort((a, b) => a.latencyMs - b.latencyMs || String(a.id).localeCompare(String(b.id)))
  const rest = list.filter((region) => !(region && region.status === 'ok' && Number.isFinite(region.latencyMs)))
  return [...ranked, ...rest]
}

export function regionLabel(regions, cluster) {
  const id = String(cluster || '').trim().toLowerCase()
  if (!id) return ''
  const known = (regions || []).find((region) => region?.id === id)
  return known?.name ? `${known.name} (${id})` : id
}

export function regionCreateMessage(detail, t) {
  const text = String(detail || '')
  const removed = text.match(/^created in (\S+) instead of (\S+); the new tunnel was removed/)
  if (removed) return t('app.modals.tunnel.regionMismatch', { actual: removed[1], requested: removed[2] })
  const kept = text.match(/^created in (\S+) instead of (\S+); the new tunnel could not be removed/)
  if (kept) return t('app.modals.tunnel.regionMismatchKept', { actual: kept[1], requested: kept[2] })
  return text
}
