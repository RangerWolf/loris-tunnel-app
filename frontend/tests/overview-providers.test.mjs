import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { createServer } from 'vite'
import { createServer as createHttpServer } from 'node:http'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'

const root = new URL('..', import.meta.url).pathname
const messages = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))

test('Overview shows two-line routes, real latency and pause actions without connection or status columns', async () => {
  const server = await createServer({
    root, server: { middlewareMode: true, hmr: { server: createHttpServer() } }, appType: 'custom',
    ssr: { noExternal: ['bootstrap'] },
    plugins: [{
      name: 'stub-bootstrap', enforce: 'pre',
      resolveId(id) { if (id === 'bootstrap') return '\0stub-bootstrap' },
      load(id) { if (id === '\0stub-bootstrap') return 'export class Tooltip {}' },
    }],
  })
  try {
    const { default: component } = await server.ssrLoadModule('/src/components/pages/OverviewPage.vue')
    const tunnels = [
      { id: 1, name: 'database', provider: 'ssh', mode: 'local', status: 'running', localHost: '127.0.0.1', localPort: 3307, remoteHost: '10.0.0.1', remotePort: 3306, latencyMs: 48 },
      { id: 2, name: 'web', provider: 'devtunnel', mode: 'http', status: 'running', localHost: '127.0.0.1', localPort: 8080, latencyMs: 123, latencyState: 'ok' },
      { id: 3, name: 'cf-web', provider: 'cloudflare', mode: 'https', status: 'running', localHost: '127.0.0.1', localPort: 8443, cloudflare: { hostname: 'dev.example.com' }, latencyState: 'timeout' },
    ]
    const app = createSSRApp(component, {
      tunnels, runningTunnels: tunnels, stoppedTunnels: [], totalTunnels: 3, autoStartCount: 0,
      logs: [], pendingTunnelActions: new Map(), getTunnelJumperLabel: () => 'ssh-jumper',
      describeDevTunnel: () => ({ connection: 'fancy-dog.usw3', url: 'https://abc-8080.usw3.devtunnels.ms/' }),
    })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
    const html = await renderToString(app)
    const header = html.match(/<thead>[\s\S]*?<\/thead>/)[0]
    assert.ok(header.includes(`<th>${messages.app.tunnels.table.latency}</th>`))
    assert.ok(!header.includes(messages.app.tunnels.table.jumper))
    assert.ok(!header.includes(`<th>${messages.app.tunnels.table.status}</th>`))
    for (const value of ['127.0.0.1:3307', '10.0.0.1:3306', 'https://abc-8080.usw3.devtunnels.ms/', 'https://dev.example.com', '48 ms', '123 ms', messages.app.tunnels.latency.timeout]) assert.ok(html.includes(value), value)
    assert.ok(!html.includes('ssh-jumper'))
    assert.ok(!html.includes('fancy-dog.usw3'))
    assert.equal((html.match(/class="tunnel-route-wrap"/g) || []).length, 3)
    assert.equal((html.match(/bi-pause/g) || []).length, 3)

  } finally { await server.close() }
})
