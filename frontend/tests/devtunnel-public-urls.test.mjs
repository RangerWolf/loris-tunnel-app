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

test('provider forms render public URLs once with copy and browser actions', async () => {
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
    const { default: component } = await server.ssrLoadModule('/src/components/tunnel-forms/DevTunnelForm.vue')
    const urls = ['https://abc-8080.usw3.devtunnels.ms/', 'https://abc.usw3.devtunnels.ms:8080/']
    const app = createSSRApp(component, { tunnelForm: { localPort: 8080, publicUrl: [...urls, urls[0].replace(/\/$/, '')].join(', '), access: 'private' } })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
    const html = await renderToString(app)
    for (const url of urls) assert.ok(html.includes(`title="${url}"`), url)
    assert.equal((html.match(/bi-copy/g) || []).length, 2)
    assert.equal((html.match(/bi-box-arrow-up-right/g) || []).length, 2)
    const { default: cloudflare } = await server.ssrLoadModule('/src/components/tunnel-forms/CloudflareTunnelForm.vue')
    for (const hostname of ['', 'baloogames.com', `${'long-host.'.repeat(12)}example.com`]) {
      const cfApp = createSSRApp(cloudflare, { tunnelForm: { hostname, tunnelIdMode: 'bind' } })
      cfApp.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
      const cfHtml = await renderToString(cfApp)
      assert.equal((cfHtml.match(/id="cloudflare-public-url"/g) || []).length, 1)
      assert.equal((cfHtml.match(/bi-copy/g) || []).length, hostname ? 1 : 0)
      assert.equal((cfHtml.match(/bi-box-arrow-up-right/g) || []).length, hostname ? 1 : 0)
      if (hostname) {
        assert.ok(cfHtml.includes(`title="https://${hostname}"`))
        assert.doesNotMatch(cfHtml, /<input[^>]*id="cloudflare-public-url"/)
      } else {
        assert.match(cfHtml, /<p[^>]*id="cloudflare-public-url"/)
      }
    }
    for (const form of [component, cloudflare]) {
      for (const state of ['expired', 'loading', 'ready']) {
        const stateApp = createSSRApp(form, {
          tunnelForm: { tunnelIdMode: 'bind', resourceNote: 'Login token expired. exit status 3' },
          resources: { state, detail: 'Login required. exit status 3', items: [] },
          accountState: 'expired',
        })
        stateApp.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
        const stateHtml = await renderToString(stateApp)
        if (state === 'ready') {
          assert.match(stateHtml, /Login token expired/)
        } else {
          assert.doesNotMatch(stateHtml, /Login token expired/)
          if (state === 'expired') {
            assert.equal((stateHtml.match(/Login required/g) || []).length, 1)
            assert.match(stateHtml, /Retry/)
          }
        }
      }
    }
  } finally { await server.close() }
})
