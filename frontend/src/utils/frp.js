import { reactive } from 'vue'
import { GetFRPStatus } from '../../wailsjs/go/main/App'

export const frpState = reactive({ installed: false, version: '', path: '', detail: '', servers: [], loading: false })
let request
export function refreshFRP() {
  if (request) return request
  frpState.loading = true
  request = GetFRPStatus().then(status => Object.assign(frpState, status, { servers: status.servers || [] }))
    .finally(() => { frpState.loading = false; request = null })
  return request
}
export function frpDomainName(value) {
  let raw = String(value || '').trim().toLowerCase().replace(/^(?:https?:\/\/)+/, '')
  raw = raw.split(/[/?#]/)[0]
  if (raw.startsWith('[')) {
    const end = raw.indexOf(']')
    return end > 1 ? raw.slice(1, end) : raw
  }
  return raw.replace(/:\d+$/, '')
}
export function frpAddress(server, mode, remotePort, domain) {
  if (!server) return ''
  const host = server.publicHost || server.host
  const formatHost = value => value.includes(':') ? `[${value}]` : value
  const name = frpDomainName(domain)
  return mode === 'http'
    ? name ? `http://${formatHost(name)}${Number(server.httpPort || 80) === 80 ? '' : `:${server.httpPort}`}` : ''
    : remotePort ? `${formatHost(host)}:${remotePort}` : ''
}
