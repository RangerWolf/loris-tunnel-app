import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'
import { createServer } from 'vite'
import { createServer as createHttpServer } from 'node:http'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'

const root = fileURLToPath(new URL('..', import.meta.url))
const messages = JSON.parse(readFileSync(new URL('../src/locales/zh-CN.json', import.meta.url), 'utf8'))

test('provider CLI summary stays compact and legacy installation locks the maintenance entry', async () => {
  const server = await createServer({
    root, server: { middlewareMode: true, hmr: { server: createHttpServer() } },
    ssr: { noExternal: ['bootstrap'] },
    plugins: [{ name: 'stub-bootstrap', enforce: 'pre',
      resolveId(id) { if (id === 'bootstrap') return '\0stub-bootstrap' },
      load(id) { if (id === '\0stub-bootstrap') return 'export class Tooltip {} export class Dropdown {}' },
    }],
  })
  try {
    for (const provider of ['CloudflareProvider', 'DevTunnelProvider']) {
      const { default: component } = await server.ssrLoadModule(`/src/components/providers/${provider}.vue`)
      for (const locale of ['zh-CN', 'en']) {
        const localeMessages = JSON.parse(readFileSync(new URL(`../src/locales/${locale}.json`, import.meta.url), 'utf8'))
        const app = createSSRApp(component, { status: { checking: true } })
        app.use(createI18n({ legacy: false, locale, messages: { [locale]: localeMessages } }))
        const html = await renderToString(app)
        assert.ok(html.includes(localeMessages.app.providers.checking))
        assert.doesNotMatch(html, /暂时无法确认状态|Status unavailable/)
        assert.doesNotMatch(html, /<button[^>]*>\s*(?:<!--[\s\S]*?-->)?\s*(?:安装|Install)\s*<\/button>/)
        assert.doesNotMatch(html, /登录后才能|登录后可以|Sign in to view/)
      }
      for (const [busy, installed, label] of [
        ['install', false, '正在安装'], ['install', true, '正在完成安装…'], ['', true, '已安装 · 2026.10.2'],
      ]) {
        const app = createSSRApp(component, { busy, status: { installed, version: '2026.10.2', accountState: 'signed_in' } })
        app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': messages } }))
        const html = await renderToString(app)
        if (label.includes(' · ')) {
          const [statusText, version] = label.split(' · ')
          assert.ok(html.includes(statusText) && html.includes(version), `${provider}: ${label}`)
        } else {
          assert.ok(html.includes(label), `${provider}: ${label}`)
        }
        if (busy) {
          assert.match(html, /class="[^"]*\bis-busy\b/)
          assert.match(html, /role="status"/)
          assert.match(html, /<button[^>]*disabled[^>]*>\s*(?:安装|检测更新)\s*<\/button>/)
          assert.ok(html.includes('安装任务结束后，检查和账号操作将自动恢复。'))
          const cliSummary = html.slice(0, html.indexOf('provider-summary-row', html.indexOf('provider-summary-row') + 1))
          assert.doesNotMatch(cliSummary, /取消|重新检测|文档/)
        } else {
          assert.doesNotMatch(html, /is-busy/)
          assert.doesNotMatch(html, /正在完成安装|安装任务结束后/)
          assert.match(html, />检测更新<\/button>/)
        }
      }
    }
  } finally { await server.close() }
})
