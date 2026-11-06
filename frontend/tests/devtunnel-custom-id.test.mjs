import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import vm from 'node:vm'
import { createServer } from 'vite'
import { createServer as createHttpServer } from 'node:http'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'

const root = new URL('..', import.meta.url).pathname
const messages = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))
// Keep concurrent test files from sharing (and cleaning) one Vite cache dir.
const cacheDir = join(tmpdir(), `loris-tunnel-vite-test-${process.pid}`)

async function renderForm(tunnelForm, extras = {}) {
  const server = await createServer({
    root, cacheDir, server: { middlewareMode: true, hmr: { server: createHttpServer() } }, appType: 'custom',
    ssr: { noExternal: ['bootstrap'] },
    plugins: [{
      name: 'stub-bootstrap', enforce: 'pre',
      resolveId(id) { if (id === 'bootstrap') return '\0stub-bootstrap' },
      load(id) { if (id === '\0stub-bootstrap') return 'export class Tooltip {}' },
    }],
  })
  try {
    const { default: component } = await server.ssrLoadModule('/src/components/tunnel-forms/DevTunnelForm.vue')
    const app = createSSRApp(component, { tunnelForm, ...extras })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
    return await renderToString(app)
  } finally { await server.close() }
}

test('custom id option stays selectable and sentinel never leaks into the missing-id option', async () => {
  const html = await renderForm({ tunnelIdMode: 'auto', access: 'private' })
  assert.match(html, /<option value="__custom__">Custom Tunnel ID<\/option>/)
  // In auto mode the select holds the empty value, never the sentinel.
  assert.match(html, /<select id="devtunnel-id" class="form-select" value="">/)
})

test('custom mode shows the name input with validation and occupancy hints', async () => {
  const base = { tunnelIdMode: 'custom', access: 'private', region: '' }
  const empty = await renderForm({ ...base, devTunnelName: '' })
  assert.match(empty, /id="devtunnel-custom-id"/)
  assert.doesNotMatch(empty, /Enter a custom Tunnel ID/)

  const invalid = await renderForm({ ...base, devTunnelName: 'my_project' })
  assert.match(invalid, /A Tunnel ID only allows 3-49 lowercase letters/)

  const short = await renderForm({ ...base, devTunnelName: 'ab' })
  assert.match(short, /A Tunnel ID only allows 3-49 lowercase letters/)

  const valid = await renderForm({ ...base, devTunnelName: 'Web-Dev-App' })
  assert.match(valid, /3-49 lowercase letters, digits, or hyphens/)
  assert.doesNotMatch(valid, /is-invalid/)
})

test('custom name conflicts with a bound or pending local service are surfaced', async () => {
  const tunnels = [
    { id: 2, name: 'other', provider: 'devtunnel', devTunnel: { tunnelId: 'taken-id.usw3', tunnelName: '' } },
    { id: 3, name: 'pending', provider: 'devtunnel', devTunnel: { tunnelId: '', tunnelName: 'planned-id' } },
  ]
  const base = { tunnelIdMode: 'custom', access: 'private' }
  const bound = await renderForm({ ...base, devTunnelName: 'taken-id' }, { tunnels, editingTunnelId: 1 })
  assert.match(bound, /already used by &quot;other&quot;/)
  const exact = await renderForm(
    { ...base, devTunnelName: 'plain-id' },
    { tunnels: [{ id: 4, name: 'plain', provider: 'devtunnel', devTunnel: { tunnelId: 'plain-id', tunnelName: '' } }], editingTunnelId: 1 },
  )
  assert.match(exact, /already used by &quot;plain&quot;/)
  const pending = await renderForm({ ...base, devTunnelName: 'planned-id' }, { tunnels, editingTunnelId: 1 })
  assert.match(pending, /already used by &quot;pending&quot;/)
  const free = await renderForm({ ...base, devTunnelName: 'free-name' }, { tunnels, editingTunnelId: 1 })
  assert.doesNotMatch(free, /already used by/)
})

test('custom name matching a remote resource id is flagged as a remote conflict', async () => {
  const html = await renderForm(
    { tunnelIdMode: 'custom', access: 'private', devTunnelName: 'remote-id' },
    { resources: { state: 'ready', detail: '', items: [{ id: 'remote-id.usw3', ports: [] }] } },
  )
  assert.match(html, /already exists in the remote account/)
})

test('bound mode keeps the readonly summary and never shows the custom input', async () => {
  const html = await renderForm({ tunnelIdMode: 'bind', tunnelId: 'abc.usw3', access: 'private', devTunnelName: '' })
  assert.match(html, /abc\.usw3/)
  assert.doesNotMatch(html, /id="devtunnel-custom-id"/)
})

const appSource = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const { normalizeHostname } = await import('../src/utils/cloudflare.js')

function buildPayload(tunnelForm) {
  const c = vm.createContext({ tunnelForm, normalizeHostname })
  const start = appSource.indexOf('function buildTunnelPayload(')
  const end = appSource.indexOf('\nfunction ', start + 10)
  vm.runInContext(appSource.slice(start, end), c)
  return c.buildTunnelPayload('stopped')
}

const baseForm = {
  provider: 'devtunnel', name: 'svc', groupId: 0, mode: 'auto', portLocked: false,
  localHost: 'http://127.0.0.1', localPort: 3000, autoStart: false,
  description: '', access: 'private', expirationAmount: 24, expirationUnit: 'h',
  region: 'usw3', tunnelId: '', tunnelIdMode: 'auto', devTunnelName: '',
}

test('devtunnel payload keeps three id modes with distinct fields and no sentinel leak', () => {
  const auto = buildPayload({ ...baseForm })
  assert.equal(auto.devTunnelIdMode, 'auto')
  assert.equal(auto.devTunnel.tunnelId, '')
  assert.equal(auto.devTunnel.tunnelName, '')
  assert.equal(auto.devTunnel.region, 'usw3')

  const custom = buildPayload({ ...baseForm, tunnelIdMode: 'custom', devTunnelName: '  Web-Dev-App  ' })
  assert.equal(custom.devTunnelIdMode, 'custom')
  assert.equal(custom.devTunnel.tunnelId, '')
  assert.equal(custom.devTunnel.tunnelName, 'web-dev-app')
  assert.equal(custom.devTunnel.region, 'usw3')
  assert.ok(!JSON.stringify(custom).includes('__custom__'))

  const bind = buildPayload({ ...baseForm, tunnelIdMode: 'bind', tunnelId: 'abc.usw3', region: '' })
  assert.equal(bind.devTunnelIdMode, 'bind')
  assert.equal(bind.devTunnel.tunnelId, 'abc.usw3')
  assert.equal(bind.devTunnel.tunnelName, '')
  assert.equal(bind.devTunnel.region, '')
})

test('cloudflare payload is unaffected by dev tunnel custom id fields', () => {
  const payload = buildPayload({
    ...baseForm, provider: 'cloudflare', tunnelName: 'cf-name', hostname: 'a.example.com',
    tunnelIdMode: 'auto', devTunnelName: '',
  })
  assert.equal(payload.devTunnel, null)
  assert.equal(payload.cloudflare.tunnelName, 'cf-name')
  assert.equal(payload.devTunnelIdMode, undefined)
})
