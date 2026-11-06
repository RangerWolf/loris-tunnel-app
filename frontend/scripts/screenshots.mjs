import assert from 'node:assert/strict'
import { mkdir, readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { chromium } from 'playwright'
import { createServer } from 'vite'

const root = fileURLToPath(new URL('../', import.meta.url))
const output = fileURLToPath(new URL('../../screenshots/', import.meta.url))
const locale = process.argv.find(arg => arg.startsWith('--locale='))?.split('=')[1] || 'en'
assert.ok(['en', 'zh-CN'].includes(locale), 'Supported screenshot locales: en, zh-CN')
const messages = JSON.parse(await readFile(new URL(`../src/locales/${locale}.json`, import.meta.url), 'utf8'))
const shots = [
  { name: 'overview', title: 'Overview' },
  ...['ssh', 'devtunnel', 'cloudflare', 'frp'].map(provider => ({ name: 'providers', title: 'Providers', provider })),
  { name: 'tunnels', title: 'Tunnels' },
  { name: 'sync', title: 'Sync' },
  { name: 'logs', title: 'Logs' },
  { name: 'config', title: 'Config' },
  { name: 'config', title: 'Config', bottom: true },
  ...(locale === 'zh-CN' ? [{ name: 'overview', title: 'Overview', accent: 'green' }] : []),
]
// Follow the desktop window dimensions instead of maintaining a second size.
const main = await readFile(new URL('../../main.go', import.meta.url), 'utf8')
const viewport = {
  width: Number(main.match(/^\s*Width:\s*(\d+),/m)?.[1]),
  height: Number(main.match(/^\s*Height:\s*(\d+),/m)?.[1]),
}
assert.ok(viewport.width > 0 && viewport.height > 0, 'Cannot read desktop window dimensions from main.go')
const server = await createServer({ root, configFile: path.join(root, 'vite.config.js'), define: { 'import.meta.env.DEV': 'false' }, server: { host: '127.0.0.1', port: 0, open: false } })
let browser
try {
  await server.listen()
  const address = server.httpServer.address()
  const origin = `http://127.0.0.1:${address.port}`
  browser = await chromium.launch({ headless: true, ...(process.env.SCREENSHOT_CHROME_PATH ? { executablePath: process.env.SCREENSHOT_CHROME_PATH } : {}) })
  const context = await browser.newContext({ viewport, deviceScaleFactor: 2, colorScheme: 'light', locale: locale === 'en' ? 'en-US' : locale, timezoneId: 'UTC' })
  // The preview must work offline and must never contact real services.
  await context.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort())
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
  await mkdir(output, { recursive: true })
  for (const { name, title, provider, bottom, accent } of shots) {
    const query = new URLSearchParams({ page: name, locale, ...(provider ? { provider } : {}) })
    await page.goto(`${origin}/screenshots/index.html?${query}`, { waitUntil: 'networkidle' })
    await page.locator('html[data-screenshot-ready="true"]').waitFor()
    await page.evaluate(() => document.fonts.ready)
    if (accent) await page.evaluate(value => { document.documentElement.dataset.accent = value }, accent)
    await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; caret-color: transparent !important; }' })
    assert.equal(await page.locator('html').getAttribute('data-theme'), 'light')
    assert.equal(await page.locator('[data-screenshot-preview]').count(), 1)
    assert.match(await page.locator('.sidebar-pro-expiry').innerText(), /Pro/, 'Screenshot must display Pro status')
    assert.doesNotMatch(await page.locator('.sidebar-version').innerText(), /-dev/, 'Screenshot must display the release version')
    assert.equal(await page.locator('.sidebar-traffic').count(), name === 'overview' ? 1 : 0, 'Only Overview should display traffic')
    assert.equal(await page.locator('html').getAttribute('lang'), locale)
    assert.equal(await page.locator('.page-title').textContent(), messages.app.sidebar[name])
    const reads = await page.evaluate(() => window.__screenshotFixtureCalls)
    assert.ok(Array.isArray(reads), 'Preview must use the standalone fixture bridge')
    assert.ok(reads.every(method => ['GetFRPStatus', 'GetCloudSyncStatus', 'GetAutoRunEnabled', 'GetTrafficMonitorEnabled', 'GetConfigLocationInfo'].includes(method)), 'Screenshot must only read fixture data')
    if (provider) {
      assert.equal(await page.locator(`#provider-tab-${provider}`).getAttribute('aria-selected'), 'true')
      assert.ok(await page.locator('main tbody tr').count() >= 2, 'Provider must show sample resources')
    }
    if (provider === 'devtunnel') {
      const text = await page.locator('#provider-panel-devtunnel').innerText()
      assert.ok(text.includes('8080') && text.includes('3000') && text.includes('asse'), 'Dev Tunnel sample ports and regions must render')
      assert.ok(!text.includes('Not fetched'), 'Dev Tunnel fixtures must contain fetched port data')
    }
    if (name === 'sync') await page.locator('.sync-status').getByText(locale === 'en' ? 'Synced' : '已同步', { exact: true }).waitFor()
    if (name === 'config') await page.locator('.config-path').waitFor({ state: 'attached' })
    if (bottom) await page.locator('main').evaluate(el => { el.scrollTop = el.scrollHeight })
    if (name === 'tunnels') {
      for (const label of ['SSH', 'Cloudflare', 'Dev Tunnel', 'FRP']) {
        assert.ok((await page.locator('main').innerText()).includes(label), `Missing provider: ${label}`)
      }
    }
    // The real Overview panels have a minimum height and may scroll at the
    // fixed desktop size. Keep that layout; verify the actual content is visible.
    const contentVisible = await page.locator('main tbody tr').evaluateAll(rows => rows.every(row => {
      const bounds = row.getBoundingClientRect()
      return bounds.top >= 0 && bounds.bottom <= innerHeight && bounds.right <= innerWidth
    }))
    assert.ok(contentVisible, `${name}: table content must fit in the desktop viewport`)
    assert.deepEqual(errors, [], 'Preview must render without errors')
    const destination = path.join(output, `screenshot-v2-${locale === 'zh-CN' ? 'cn-' : ''}${name}${provider ? `-${provider}` : ''}${bottom ? '-license' : ''}${accent ? `-${accent}` : ''}-light.png`)
    await page.screenshot({ path: destination, animations: 'disabled' })
    console.log(`Saved ${destination}`)
  }
} finally {
  await browser?.close()
  await server.close()
}
