import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { createServer } from 'vite'
import { createServer as createHttpServer } from 'node:http'
import { createSSRApp, reactive } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'

const root = new URL('..', import.meta.url).pathname
const messages = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))

test('both pages render spinner and disabled action only while pending', async () => {
  const server = await createServer({
    root, server: { middlewareMode: true, hmr: { server: createHttpServer() } }, appType: 'custom',
    ssr: { noExternal: ['bootstrap'] },
    plugins: [{
      name: 'test-bootstrap', enforce: 'pre',
      resolveId(id) { if (id === 'bootstrap') return '\0test-bootstrap' },
      load(id) {
        if (id === '\0test-bootstrap') return 'export class Tooltip {} export class Dropdown {}'
      }
    }]
  })
  try {
    const tunnel = { id: 1, name: 'test', status: 'running', mode: 'local', jumperIds: [], localHost: '127.0.0.1', localPort: 3307, remoteHost: '127.0.0.1', remotePort: 3306 }
    for (const page of ['TunnelsPage', 'OverviewPage']) {
      const { default: component } = await server.ssrLoadModule(`/src/components/pages/${page}.vue`)
      const pendingTunnelActions = reactive(new Map())
      const props = {
        pendingTunnelActions, tunnels: [tunnel], groups: [], modeOptions: [], tunnelAiDebugStates: {},
        totalTunnels: 1, runningTunnels: [tunnel], stoppedTunnels: [], autoStartCount: 0,
        logs: [], getTunnelJumperLabel: () => 'test'
      }
      async function render() {
        const app = createSSRApp(component, props)
        app.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
        return renderToString(app)
      }
      assert.doesNotMatch(await render(), /bi-arrow-repeat spin/, page)
      pendingTunnelActions.set(1, 'stop')
      const html = await render()
      assert.match(html, /<button[^>]*disabled[^>]*>\s*<i[^>]*bi-arrow-repeat spin/, page)
      assert.match(html, /test/, page)
      pendingTunnelActions.delete(1)
      assert.doesNotMatch(await render(), /bi-arrow-repeat spin/, page)
    }
  } finally {
    await server.close()
  }
})
