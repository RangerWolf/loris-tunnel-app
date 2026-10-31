// Fictional display data only. These addresses and states never start a connection.
export const tunnels = [
  { id: 1, name: 'Staging Database', provider: 'ssh', mode: 'local', status: 'running', localHost: '127.0.0.1', localPort: 15432, remoteHost: '127.0.0.1', remotePort: 5432, autoStart: true, latencyMs: 24 },
  { id: 2, name: 'Website Preview', provider: 'cloudflare', mode: 'http', status: 'running', localHost: '127.0.0.1', localPort: 3000, autoStart: true, publicUrl: 'https://preview.example.com', cloudflare: { tunnelId: 'web-preview', hostname: 'preview.example.com' }, latencyState: 'ok', latencyMs: 68 },
  { id: 3, name: 'API Development', provider: 'devtunnel', mode: 'http', status: 'running', localHost: '127.0.0.1', localPort: 8080, autoStart: false, publicUrl: 'https://api.example.com', devTunnel: { tunnelId: 'api-dev.asse', access: 'private' }, latencyState: 'ok', latencyMs: 42 },
  { id: 4, name: 'FRP TCP Service', provider: 'frp', mode: 'tcp', status: 'stopped', localHost: '127.0.0.1', localPort: 9000, autoStart: false, frp: { serverId: 'sample-server', remotePort: 19000 } },
]

export const logs = [
  { id: 1, time: '10:24:35', level: 'info', message: 'API Development · Dev Tunnel connected' },
  { id: 2, time: '10:23:16', level: 'info', message: 'Website Preview · Cloudflare connected' },
  { id: 3, time: '10:22:04', level: 'info', message: 'Staging Database · SSH forwarding started' },
  { id: 4, time: '10:21:48', level: 'info', message: 'FRP TCP Service · configuration saved' },
]

export const trafficHistoryUp = [2, 2, 3, 2, 42, 5, 2, 2, 3, 2, 2, 54, 4, 2, 2, 3, 2, 38, 4, 2, 2, 2, 3, 2, 46].map(n => n * 1024)
export const trafficHistoryDown = [3, 3, 4, 3, 118, 8, 3, 3, 4, 3, 3, 145, 7, 3, 3, 4, 3, 106, 7, 3, 3, 3, 4, 3, 128].map(n => n * 1024)

export const jumpers = [
  { id: 1, name: 'Staging SSH', host: 'staging.example.com', port: 22, user: 'developer', authType: 'ssh_key', keyPath: '/Users/demo/.ssh/id_ed25519' },
  { id: 2, name: 'Development SSH', host: 'dev.example.com', port: 22, user: 'developer', authType: 'ssh_agent' },
]
tunnels[0].jumperIds = [1]

export const frpStatus = { installed: true, version: '0.71.1', path: '/Users/demo/Library/Application Support/loris-tunnel/frpc', servers: [
  { id: 'sample-server', name: 'Dev FRP', host: 'frp.example.com', port: 7000, publicHost: 'frp.example.com', tls: true },
  { id: 'staging-server', name: 'Staging FRP', host: 'staging.example.com', port: 7000, publicHost: 'staging.example.com', tls: true },
] }
export const devTunnelStatus = { installed: true, version: '1.0.1500', accountState: 'signed_in', accountLabel: 'developer@example.com' }
export const devTunnelResources = { state: 'ready', items: [
  { id: 'api-dev.asse', ports: [{ port: 8080, protocol: 'http' }], portsKnown: true, access: 'private', expiration: '2026-11-24T10:00:00Z' },
  { id: 'web-preview.asse', ports: [{ port: 3000, protocol: 'http' }], portsKnown: true, access: 'public', expiration: '2026-11-24T10:00:00Z' },
] }
export const cloudflareStatus = { installed: true, version: '2026.10.0', accountState: 'signed_in' }
export const cloudflareResources = { state: 'ready', items: [
  { id: 'web-preview', name: 'Website Preview', credentialReady: true, connections: 4 },
  { id: 'staging-api', name: 'Staging API', credentialReady: true, connections: 0 },
] }
export const cloudflareZones = { state: 'ready', items: [
  { id: 'example-zone', name: 'example.com', state: 'ready', linkedTunnels: 1 },
  { id: 'example-org-zone', name: 'example.org', state: 'ready', linkedTunnels: 0 },
] }
export const syncStatus = {
  enabled: true, connected: true, state: 'synced', automatic: true,
  device_name: 'Demo MacBook', server_url: 'https://sync.example.com',
  revision: 12, last_synced_revision: 12,
  last_synced_at: '2026-10-25T10:24:00Z', update_time: '2026-10-25T10:24:00Z', updated_by: 'Demo MacBook',
}
export const activityLogs = [...logs,
  { id: 5, time: '10:20:30', level: 'info', message: 'Cloud Sync · revision 12 synchronized' },
  { id: 6, time: '10:19:42', level: 'warn', message: 'Staging SSH · reconnecting after network change' },
  { id: 7, time: '10:19:40', level: 'error', message: 'Staging SSH · connection interrupted' },
  { id: 8, time: '10:18:00', level: 'info', message: 'Application started · configuration loaded' },
]
