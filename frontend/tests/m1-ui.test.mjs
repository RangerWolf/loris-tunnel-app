import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { test } from 'node:test'
import { createServer } from 'vite'
import { createServer as createHttpServer } from 'node:http'
import { createSSRApp, reactive } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'

const root = new URL('..', import.meta.url).pathname
const messages = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))

const tunnels = [
  {
    id: 1, name: 'local-db', status: 'running', latencyMs: 48, mode: 'local',
    groupId: 0, jumperIds: [1], localHost: '127.0.0.1', localPort: 3307,
    remoteHost: '127.0.0.1', remotePort: 3306, autoStart: true
  },
  {
    id: 2, name: 'staging', status: 'error', lastError: 'key rejected', mode: 'remote',
    groupId: 0, jumperIds: [1], localHost: '127.0.0.1', localPort: 2222,
    remoteHost: '10.0.0.1', remotePort: 22, autoStart: false
  }
]

test('M1 Overview and Tunnels display real SSH-only state and preserve actions', async () => {
  const server = await createServer({
    root,
    server: { middlewareMode: true, hmr: { server: createHttpServer() } },
    appType: 'custom',
    ssr: { noExternal: ['bootstrap'] },
    plugins: [{
      name: 'test-bootstrap',
      enforce: 'pre',
      resolveId(id) { if (id === 'bootstrap') return '\0test-bootstrap' },
      load(id) {
        if (id === '\0test-bootstrap') return 'export class Tooltip {} export class Dropdown {}'
      }
    }]
  })

  try {
    const pendingTunnelActions = reactive(new Map())
    const base = {
      pendingTunnelActions,
      tunnels,
      logs: [{ id: 1, level: 'error', time: '09:30', message: 'staging: key rejected' }],
      getTunnelJumperLabel: () => 'bastion'
    }
    const pages = [
      {
        name: 'OverviewPage',
        props: {
          ...base,
          totalTunnels: tunnels.length,
          runningTunnels: [tunnels[0]],
          stoppedTunnels: [tunnels[1]],
          autoStartCount: 1
        },
        required: ['local-db', 'Active Tunnels', 'Manage tunnels', 'Failed'],
      },
      {
        name: 'TunnelsPage',
        props: { ...base, groups: [], modeOptions: [{ value: 'local', label: 'Local' }, { value: 'remote', label: 'Remote' }], tunnelAiDebugStates: {} },
        required: ['local-db', 'staging', 'SSH', 'Group by', 'My groups', 'Error']
      }
    ]
    for (const page of pages) {
      const { default: component } = await server.ssrLoadModule('/src/components/pages/' + page.name + '.vue')
      const app = createSSRApp(component, page.props)
      app.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
      const html = await renderToString(app)
      for (const text of page.required) assert.ok(html.includes(text), page.name + ' missing ' + text)
      assert.doesNotMatch(html, /\b(?:Cloudflared|Pinggy|FRP)\b/)
      if (page.name === 'TunnelsPage') {
        assert.doesNotMatch(html, /lt-tunnel-name-button|lt-details-drawer/, 'Tunnel names must not open a details drawer')
        assert.match(html, /class="lt-tunnel-name"/, 'Tunnel names remain visible as plain text')
      }
      if (page.name === 'OverviewPage') {
        const dotClasses = [...html.matchAll(/class="lt-status-dot (running|error|stopped|auto-start)"/g)].map(match => match[1])
        assert.deepEqual(dotClasses, ['running', 'error', 'stopped', 'auto-start'], 'Each metric uses its semantic status dot')
        const activeTable = html.match(/<table[^>]*lt-overview-tunnel-table[^>]*>[\s\S]*?<\/table>/)?.[0]
        assert.ok(activeTable, 'Overview active table missing')
        assert.match(activeTable, /local-db/)
        assert.doesNotMatch(activeTable, /staging|SSH|48 ms/, 'Overview must only show active tunnels, without the clipped type badge')
        assert.doesNotMatch(html, /lt-filter-chips|lt-overview-toolbar/, 'Overview must not duplicate tunnel management filters')
        assert.doesNotMatch(html, /lt-overview-alert|1 tunnel\(s\) have an error/, 'Overview must not render an error banner')
        const emptyApp = createSSRApp(component, { ...page.props, runningTunnels: [] })
        emptyApp.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
        const emptyHtml = await renderToString(emptyApp)
        assert.match(emptyHtml, /class="lt-overview-empty-row"/, 'Empty tunnel row keeps the bottom divider')
        assert.match(emptyHtml, /No running tunnels/)
        assert.match(html, /<span class="panel-title">Active Tunnels<\/span>/)
        assert.match(html, /<span class="panel-title">Recent Activity<\/span>/)
        assert.match(html, /staging: key rejected/, 'Recent activity always renders')
        assert.doesNotMatch(html, /overview-title-btn|bi-chevron-(?:up|down)|aria-expanded|is-collapsed|display:none/, 'Overview panels have no collapse controls or hidden state')
      }
    }
    const { default: AppTopHeader } = await server.ssrLoadModule('/src/components/layout/AppTopHeader.vue')
    const headerHtml = async (activePage) => {
      const app = createSSRApp(AppTopHeader, { activePage, currentPage: { title: 'Test', subtitle: '' } })
      app.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
      return renderToString(app)
    }
    assert.doesNotMatch(await headerHtml('overview'), /New Tunnel/, 'Overview should not have a New Tunnel button')
    assert.match(await headerHtml('tunnels'), /New Tunnel/, 'Tunnels should keep its New Tunnel button')

    const { default: AppSidebar } = await server.ssrLoadModule('/src/components/layout/AppSidebar.vue')
    for (const [collapsed, symbol] of [[false, '‹'], [true, '›']]) {
      const app = createSSRApp(AppSidebar, {
        pages: [], activePage: 'overview', appVersion: '1', isPro: false,
        proExpiryLabel: '', collapsed, trafficMonitorEnabled: false
      })
      app.use(createI18n({ legacy: false, locale: 'en', messages: { en: messages } }))
      const html = await renderToString(app)
      const button = html.match(/<button[^>]*class="sidebar-collapse-btn"[^>]*>[\s\S]*?<\/button>/)?.[0]
      assert.ok(button, 'Sidebar must use its own compact button, not the Bootstrap button class')
      assert.ok(button.includes(symbol), 'Sidebar must render the prototype arrow character')
      assert.doesNotMatch(button, /bi-chevron|bi-layout-sidebar/, 'Sidebar should not use a Bootstrap icon')
      if (!collapsed) {
        assert.match(html, /v1-dev/, 'Dev server sidebar version must include the -dev suffix')
        assert.match(html, /class="(?:dev-mode sidebar-footer|sidebar-footer dev-mode)"/, 'Dev footer must allow a longer version label')
      }
    }
  } finally {
    await server.close()
  }
})

test('sidebar toggle arrow is centered inside the compact outlined button', () => {
  const css = readFileSync(new URL('../src/styles/app-shell.css', import.meta.url), 'utf8')
  assert.match(css, /\.sidebar-collapse-btn\s*\{[^}]*display:\s*inline-grid;[^}]*place-items:\s*center;/)
  assert.match(css, /\.sidebar-collapse-btn\s*\{[^}]*width:\s*27px;[^}]*height:\s*27px;[^}]*padding:\s*0;/)
  assert.match(css, /\.sidebar-collapse-btn\s*\{[^}]*border:\s*1px solid var\(--lt-border\);/)
  assert.match(css, /\.sidebar-footer\.dev-mode\s*\{[^}]*flex-wrap:\s*wrap;/)
})

test('Overview metrics stay compact and both always-visible panels retain 300px minimum height', () => {
  const css = readFileSync(new URL('../src/styles/ssh-ui-m1.css', import.meta.url), 'utf8')
  assert.match(css, /\.lt-overview-metrics\s*\{[^}]*grid-template-columns:\s*repeat\(4,\s*minmax\(0,\s*1fr\)\)/)
  assert.match(css, /\.lt-overview-metrics \.metric-label\s*\{[^}]*align-items:\s*center;/)
  assert.match(css, /\.lt-overview-metrics \.lt-status-dot\.auto-start\s*\{\s*background:\s*var\(--bs-blue\);/)
  assert.match(css, /\.lt-overview-list,\s*\.lt-overview-activity\s*\{[^}]*min-height:\s*300px;/)
  assert.doesNotMatch(css, /\.lt-overview-(?:list|activity)\.is-collapsed/, 'No collapsed panel height override remains')
  assert.match(css, /\.lt-overview-list-body\s*\{[^}]*flex:\s*1 1 auto;/)
  assert.match(css, /\.lt-overview-pagination\s*\{[^}]*margin-top:\s*auto;/)
  assert.match(css, /\.lt-overview-tunnel-table tbody tr:last-child:not\(\.lt-overview-empty-row\) td\s*\{\s*border-bottom:\s*0;/)
})

test('source UI contains no gradient declarations', () => {
  const src = new URL('../src', import.meta.url).pathname
  const inspect = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name)
      if (entry.isDirectory()) inspect(path)
      if (!entry.isFile() || !/\.(css|vue)$/.test(entry.name)) continue
      const content = readFileSync(path, 'utf8')
      assert.doesNotMatch(content, /\b(?:linear|radial|conic|repeating-linear|repeating-radial)-gradient\s*\(|<\s*(?:linearGradient|radialGradient)\b/i, path)
    }
  }
  inspect(src)
  assert.match(readFileSync(new URL('../../AGENTS.md', import.meta.url), 'utf8'), /禁止渐变色/)
})
