import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'

const root = fileURLToPath(new URL('..', import.meta.url))
const messages = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))

test('FRP reuses provider controls and renders TCP, HTTP, missing-server and reconnect states', async () => {
  const vite = await createServer({
    root, logLevel: 'error', server: { middlewareMode: true },
    optimizeDeps: { disabled: true }, ssr: { noExternal: ['bootstrap'] },
    plugins: [{ name: 'frp-test-bootstrap', enforce: 'pre',
      resolveId(id) { if (id === 'bootstrap') return '\0frp-test-bootstrap' },
      load(id) { if (id === '\0frp-test-bootstrap') return 'export class Tooltip {} export class Dropdown {}' },
    }],
  })
  try {
    const { frpState, frpAddress } = await vite.ssrLoadModule('/src/utils/frp.js')
    const server = { id: 'local', name: 'Hong Kong', host: 'frp.example.com', port: 7000, httpPort: 8080, hasToken: true }
    Object.assign(frpState, { installed: true, version: '0.71.1', servers: [server] })
    async function render(path, props) {
      const component = (await vite.ssrLoadModule(path)).default
      const app = createSSRApp(component, props)
      app.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
      return renderToString(app)
    }
    const providers = await render('/src/components/pages/ProvidersPage.vue', { tab: 'frp', jumpers: [], tunnels: [] })
    assert.match(providers, /provider-tab-frp/)
    assert.match(providers, /provider-summary-label/)
    assert.match(providers, /Check for updates/)
    assert.match(providers, /provider-summary-status/)
    assert.match(providers, /provider-tool-btn/)
    assert.match(providers, /provider-frp-table/)
    assert.match(providers, /provider-col-actions/)
    assert.match(providers, />Action</)
    assert.match(providers, /provider-toolbar/)
    assert.match(providers, /provider-row-actions/)
    assert.match(providers, /Hong Kong/)
    assert.match(providers, /0\.71\.1/)
    assert.match(providers, /Not linked/)
    assert.doesNotMatch(providers, /Configured locally/)
    assert.doesNotMatch(providers, /type="password"/)

    const form = { provider: 'frp', mode: 'tcp', localHost: '127.0.0.1', localPort: 3000, frpServerId: 'local', frpProxyName: 'web', frpRemotePort: 13000, frpDomain: '' }
    let html = await render('/src/components/tunnel-forms/FRPTunnelForm.vue', { tunnelForm: form })
    assert.match(html, /tcp:\/\/127.0.0.1:3000/)
    assert.match(html, /frp.example.com:13000/)
    assert.match(html, /frp-remote/)
    assert.match(html, /id="frp-proxy"/)
    assert.doesNotMatch(html, /id="frp-domain"/)
    html = await render('/src/components/tunnel-forms/FRPTunnelForm.vue', { tunnelForm: { ...form, mode: 'http', frpDomain: 'dev.example.com' } })
    assert.match(html, /http:\/\/dev.example.com:8080/)
    assert.match(html, /id="frp-domain"/)
    assert.doesNotMatch(html, /id="frp-remote"/)
    html = await render('/src/components/tunnel-forms/FRPTunnelForm.vue', { tunnelForm: { ...form, frpServerId: 'other-device' } })
    assert.match(html, /Configure this server on this device/)

    const tunnel = { id: 1, provider: 'frp', frp: { serverId: 'local' }, mode: 'tcp', localHost: '127.0.0.1', localPort: 3000, name: 'web', status: 'reconnecting', lastError: 'connection interrupted', publicUrl: 'frp.example.com:13000' }
    html = await render('/src/components/pages/TunnelsPage.vue', { tunnels: [{ ...tunnel, groupId: 1 }], pendingTunnelActions: new Map(), groups: [{ id: 1, name: 'Development' }], modeOptions: [], tunnelAiDebugStates: {}, getTunnelJumperLabel: () => server.name })
    assert.match(html, /Reconnecting/)
    assert.match(html, /bi-pause-fill/)
    assert.match(html, /frp.example.com:13000/)
    html = await render('/src/components/pages/OverviewPage.vue', { tunnels: [tunnel], runningTunnels: [tunnel], stoppedTunnels: [], pendingTunnelActions: new Map(), totalTunnels: 1, autoStartCount: 0, logs: [], getTunnelJumperLabel: () => server.name })
    assert.match(html, /frp.example.com:13000/)
    assert.doesNotMatch(html, /undefined:undefined/)
    assert.equal(frpAddress({ ...server, publicHost: '::1' }, 'tcp', 13000, ''), '[::1]:13000')
    assert.equal(frpAddress(server, 'http', 0, 'http://dev.example.com:8080'), 'http://dev.example.com:8080')
  } finally { await vite.close() }
})
