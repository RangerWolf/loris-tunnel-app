import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { test } from 'node:test'
import { createServer } from 'vite'
import { createServer as createHttpServer } from 'node:http'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'

const root = new URL('..', import.meta.url).pathname
const zh = JSON.parse(readFileSync(new URL('../src/locales/zh-CN.json', import.meta.url), 'utf8'))
const en = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))

function appFor(component, props, locale = 'zh-CN') {
  const app = createSSRApp(component, props)
  app.use(createI18n({
    legacy: false,
    locale,
    messages: { 'zh-CN': zh, en },
  }))
  return app
}

function visibleText(html) {
  return html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ')
}

test('provider pages share one resource layout and keep existing actions', async () => {
  const server = await createServer({
    root,
    server: { middlewareMode: true, hmr: { server: createHttpServer() } },
    appType: 'custom',
    ssr: { noExternal: ['bootstrap'] },
    plugins: [{
      name: 'stub-bootstrap',
      enforce: 'pre',
      resolveId(id) { if (id === 'bootstrap') return '\0stub-bootstrap' },
      load(id) { if (id === '\0stub-bootstrap') return 'export class Tooltip {} export class Dropdown {}' },
    }],
  })
  try {
    const { default: JumpersPage } = await server.ssrLoadModule('/src/components/pages/JumpersPage.vue')
    const jumpers = [
      { id: 1, name: 'flyml-net', host: 'www.flyml.net', port: 22, user: 'ubuntu', authType: 'ssh_key', keyPath: '~/ssh/loris_service.pem', notes: 'from laptop', password: 's3cret-password' },
      { id: 2, name: 'flyml-db', host: '114.0.0.8', port: 22, user: 'ubuntu', authType: 'password', password: 's3cret-password' },
      { id: 3, name: 'agent-box', host: '2001:db8::1', port: 22, user: 'root', authType: 'ssh_agent' },
      { id: 4, name: 'win-box', host: '10.0.0.4', port: 2222, user: 'ops', authType: 'ssh_key', keyPath: 'C:\\keys\\id_rsa' },
      { id: 5, name: 'mystery', host: '10.0.0.5', port: 22, user: 'ops', authType: 'other' },
    ]
    const tunnels = [
      { id: 11, name: 'alpha', provider: 'ssh', status: 'running', jumperIds: [1] },
      { id: 12, name: 'beta', provider: 'ssh', status: 'stopped', jumperIds: [1, 1] },
      { id: 13, name: 'gamma', provider: 'ssh', status: 'error', jumperIds: ['1'] },
      { id: 14, name: 'delta', provider: 'devtunnel', status: 'running', jumperIds: [1] },
      { id: 15, name: 'epsilon', provider: 'ssh', status: 'running', jumperIds: [2] },
    ]
    const html = await renderToString(appFor(JumpersPage, {
      jumpers, totalCount: 6, tunnels, searchQuery: 'zzz',
    }))
    const row = html.match(/flyml-net[\s\S]*?<\/tr>/)?.[0] || ''
    assert.ok(row.indexOf('www.flyml.net:22') >= 0)
    assert.ok(row.indexOf('用户名 ubuntu') > row.indexOf('www.flyml.net:22'))
    assert.match(row, /data-bs-title="[^"]*loris_service\.pem/)
    assert.match(row, />loris_service\.pem</)
    assert.match(html, /\[2001:db8::1\]:22/)
    assert.match(html, />密钥</)
    assert.match(html, />密码</)
    assert.match(html, />SSH Agent</)
    assert.match(html, />未知</)
    assert.match(html, /id_rsa/)
    assert.doesNotMatch(html, /s3cret-password/)
    assert.doesNotMatch(html, /SSH 密钥/)
    assert.doesNotMatch(visibleText(html), /from laptop/)
    assert.match(html, /from laptop/)
    assert.match(html, /3 条关联/)
    assert.match(html, /alpha, beta, gamma/)
    assert.match(html, /1 条关联/)
    assert.match(html, />未关联</)
    assert.match(html, /克隆配置/)
    assert.match(html, /bi-pencil/)
    assert.doesNotMatch(html, /bi-copy|bi-trash3|复制到剪贴板/)
    assert.match(html, /匹配 5 \/ 共 6/)
    assert.match(html, /flyml-db/)
    const empty = await renderToString(appFor(JumpersPage, { jumpers: [], totalCount: 0, tunnels: [], searchQuery: '' }))
    assert.match(empty, /暂无跳板机，可新建或导入配置。/)
    const missed = await renderToString(appFor(JumpersPage, { jumpers: [], totalCount: 6, tunnels: [], searchQuery: 'none' }))
    assert.match(missed, /没有匹配的结果。/)
    assert.match(missed, /清空搜索/)

    const source = readFileSync(new URL('../src/components/pages/JumpersPage.vue', import.meta.url), 'utf8')
    assert.match(source, /emit\('edit-jumper'/)
    assert.match(source, /emit\('copy-jumper', jumper\)/)
    assert.match(source, /emit\('delete-jumper', jumper\)/)
    assert.doesNotMatch(source, /\.filter\(/)

    const appSource = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
    const start = appSource.indexOf('function jumperMatchesQuery(')
    const matcher = appSource.slice(start, appSource.indexOf('const filteredJumpers', start))
    const context = vm.createContext({})
    vm.runInContext(`${matcher} this.matches = jumperMatchesQuery`, context)
    const sample = jumpers[0]
    assert.equal(context.matches(sample, 'www.flyml.net:22'), true)
    assert.equal(context.matches(sample, 'from laptop'), true)
    assert.equal(context.matches(sample, 'missing-host'), false)
    assert.equal(context.matches({ ...jumpers[2], notes: '' }, '[2001:db8::1]:22'), true)

    const { default: ProvidersPage } = await server.ssrLoadModule('/src/components/pages/ProvidersPage.vue')
    const page = await renderToString(appFor(ProvidersPage, {
      tab: 'ssh', jumpers, jumperTotal: jumpers.length, searchQuery: '', tunnels,
    }))
    assert.match(page, /role="tablist"/)
    assert.match(page, /role="tab"/)
    assert.match(page, /aria-selected="true"/)
    assert.match(page, /aria-controls="provider-panel-ssh"/)
    assert.match(page, /role="tabpanel"/)
    assert.doesNotMatch(page, /Tailscale/)
    const providersSource = readFileSync(new URL('../src/components/pages/ProvidersPage.vue', import.meta.url), 'utf8')
    assert.match(providersSource, /selectTab\('cloudflare'\)/)
    assert.doesNotMatch(providersSource, /cloudflare[\s\S]{0,80}disabled/)
    assert.match(providersSource, /ArrowLeft/)
    assert.match(providersSource, /devSearchQuery/)
    assert.match(providersSource, /cloudflareSearchQuery/)

    const { default: Header } = await server.ssrLoadModule('/src/components/layout/AppTopHeader.vue')
    const providersHeader = await renderToString(appFor(Header, { activePage: 'providers', currentPage: { title: '提供方', subtitle: '管理连接提供方' } }, 'zh-CN'))
    assert.doesNotMatch(providersHeader, /新建跳板机|导入/)
    const tunnelsHeader = await renderToString(appFor(Header, { activePage: 'tunnels', currentPage: { title: '隧道', subtitle: '' } }, 'en'))
    assert.match(tunnelsHeader, /New Tunnel/)
    assert.doesNotMatch(readFileSync(new URL('../src/components/layout/AppTopHeader.vue', import.meta.url), 'utf8'), /import-jumper|new-jumper/)
    assert.match(appSource, /@import-jumper="openImportJumper"/)
    assert.match(appSource, /@new-jumper="openNewJumper"/)

    const { default: Dev } = await server.ssrLoadModule('/src/components/providers/DevTunnelProvider.vue')
    const ready = {
      installed: true,
      version: '1.2.3',
      accountState: 'signed_in',
      accountLabel: 'ada@github',
    }
    const devItems = [
      { id: 'abc.usw3', access: 'public', portsKnown: true, ports: [{ port: 80 }, { port: 443 }, { port: 8080 }] },
      { id: 'other', access: 'private', portsKnown: true, ports: [] },
    ]
    const devTunnels = [
      { id: 1, name: 'web', provider: 'devtunnel', devTunnel: { tunnelId: 'abc' } },
      { id: 2, name: 'api', provider: 'devtunnel', devTunnel: { tunnelId: 'abc.usw3' } },
    ]
    const devHtml = await renderToString(appFor(Dev, {
      status: ready,
      resources: { state: 'ready', detail: '', items: devItems },
      tunnels: devTunnels,
      searchQuery: '',
    }))
    assert.match(devHtml, /provider-summary/)
    assert.doesNotMatch(devHtml, /devtunnel-section/)
    assert.match(devHtml, /已安装/)
    assert.match(devHtml, /1\.2\.3/)
    assert.match(devHtml, /已登录/)
    assert.match(devHtml, /ada@github/)
    assert.match(devHtml, /账号操作/)
    assert.match(devHtml, /80, 443 \+1/)
    assert.match(devHtml, /title="80, 443, 8080"/)
    assert.match(devHtml, /2 条关联/)
    assert.match(devHtml, /web, api/)
    assert.match(devHtml, />公开</)
    assert.match(devHtml, />未配置</)
    assert.match(devHtml, /复制 Tunnel ID/)
    assert.match(devHtml, /使用此 ID 新建隧道/)
    assert.match(devHtml, /删除 Tunnel ID/)
    assert.match(devHtml, />详情<\/button>/)
    const filtered = await renderToString(appFor(Dev, {
      status: ready,
      resources: { state: 'ready', detail: '', items: devItems },
      tunnels: devTunnels,
      searchQuery: 'other',
    }))
    assert.match(filtered, />other</)
    assert.doesNotMatch(filtered, /abc\.usw3/)
    assert.match(filtered, /匹配 1 \/ 共 2/)
    const devSource = readFileSync(new URL('../src/components/providers/DevTunnelProvider.vue', import.meta.url), 'utf8')
    assert.doesNotMatch(devSource, /update-search-query[\s\S]{0,80}refresh-resources/)

    const loading = await renderToString(appFor(Dev, {
      status: ready,
      resources: { state: 'loading', detail: '', items: [] },
    }))
    assert.match(loading, /正在读取/)
    assert.match(loading, /· —/)
    assert.doesNotMatch(loading, /暂无 Tunnel ID/)

    const stale = await renderToString(appFor(Dev, {
      status: ready,
      resources: { state: 'error', detail: 'cli failed', items: [{ id: 'kept-id', access: 'private', ports: [] }] },
    }))
    assert.match(stale, /kept-id/)
    assert.match(stale, /cli failed/)
    assert.match(stale, /当前显示上次读取的数据/)
    assert.doesNotMatch(stale, /暂无 Tunnel ID/)

    const signedOut = await renderToString(appFor(Dev, {
      status: { installed: true, accountState: 'signed_out' },
      resources: { state: 'ready', detail: '', items: [{ id: 'cached-secret-id', access: 'public', ports: [] }] },
    }))
    assert.doesNotMatch(signedOut, /cached-secret-id/)
    assert.match(signedOut, /登录后可以查看和创建 Tunnel ID/)
    assert.match(signedOut, /disabled[\s\S]{0,80}\+ 新建 ID|\+ 新建 ID/)
    assert.match(signedOut, /<button[^>]*disabled[^>]*>[\s\S]*?\+ 新建 ID\s*<\/button>/)

    const login = await renderToString(appFor(Dev, {
      busy: 'login:github',
      status: { installed: true, accountState: 'signed_out', loginInProgress: true, loginPhase: 'browser', loginUrl: 'https://login.example/dev' },
    }))
    assert.match(login, /登录中/)
    assert.match(login, /取消/)
    assert.match(login, /重新打开浏览器/)

    const { default: Cloudflare } = await server.ssrLoadModule('/src/components/providers/CloudflareProvider.vue')
    const cfHtml = await renderToString(appFor(Cloudflare, {
      status: { installed: true, version: '2026.1', accountState: 'signed_in' },
      zones: { state: 'ready', items: [{ name: 'example.com' }, { name: 'other.com' }] },
      resources: { state: 'ready', items: [{ id: '0123456789abcdef', name: 'edge', credentialReady: false }] },
      tunnels: [{ id: 9, name: 'site', provider: 'cloudflare', cloudflare: { tunnelId: '0123456789abcdef' } }],
    }))
    assert.match(cfHtml, /已认证/)
    assert.match(cfHtml, /example\.com[\s\S]*other\.com/)
    assert.match(cfHtml, /可用|缺失/)
    assert.match(cfHtml, />缺失</)
    assert.doesNotMatch(cfHtml, /未认证/)
    assert.match(cfHtml, /添加域名/)
    assert.match(cfHtml, /管理域名/)
    assert.match(cfHtml, /1 条关联/)
    assert.match(cfHtml, /0123…def/)
    assert.match(cfHtml, /0123456789abcdef/)
    assert.match(cfHtml, /<button[^>]*disabled[^>]*>\s*使用此 ID 新建隧道\s*<\/button>/)
    const domainProps = {
      status: { installed: true, accountState: 'signed_in' },
      zones: { state: 'ready', items: [{ name: 'authorized.example' }, { name: 'second.example' }, { name: 'third.example' }] },
      resources: { state: 'ready', items: [{ id: 'cf-one', name: 'first' }, { id: 'cf-two', name: 'second' }] },
      tunnels: [
        { provider: 'cloudflare', cloudflare: { tunnelId: 'CF-ONE', hostname: 'Alpha.example.' } },
        { provider: 'cloudflare', cloudflare: { tunnelId: 'cf-one', hostname: 'alpha.example' } },
        { provider: 'cloudflare', cloudflare: { tunnelId: 'cf-one', hostname: 'beta.example' } },
        { provider: 'cloudflare', cloudflare: { tunnelId: 'cf-one', hostname: 'hidden.example' } },
        { provider: 'cloudflare', cloudflare: { tunnelId: 'cf-two', hostname: 'other.example' } },
      ],
    }
    const domainsHtml = await renderToString(appFor(Cloudflare, domainProps))
    const firstRow = domainsHtml.match(/>first<[\s\S]*?<\/tr>/)[0]
    assert.equal((firstRow.match(/>alpha\.example</g) || []).length, 1)
    assert.match(firstRow, /beta\.example/)
    assert.match(firstRow, /另 1 个/)
    assert.doesNotMatch(firstRow, /other\.example|authorized\.example/)
    assert.match(domainsHtml, /可用域名（3）/)
    assert.match(domainsHtml, /… \+1/)
    assert.doesNotMatch(domainsHtml, /third\.example/)
    const domainFiltered = await renderToString(appFor(Cloudflare, { ...domainProps, searchQuery: 'hidden.example' }))
    assert.match(domainFiltered, />first</)
    assert.doesNotMatch(domainFiltered, />second</)
    const updated = await renderToString(appFor(Cloudflare, { ...domainProps, tunnels: [] }))
    assert.doesNotMatch(updated, /alpha\.example/)
    assert.match(updated, /没有本地配置记录，不代表线上未绑定域名/)
    for (const [state, text] of [['loading', '正在读取可用域名'], ['error', '可用域名读取失败']]) {
      const html = await renderToString(appFor(Cloudflare, { ...domainProps, zones: { state, items: [] } }))
      assert.match(html, new RegExp(text))
      assert.match(html, /已认证/)
    }
    const cfOut = await renderToString(appFor(Cloudflare, {
      status: { installed: true, accountState: 'signed_out' },
      resources: { state: 'ready', items: [{ id: 'cached-cf-id', name: 'old', credentialReady: false }] },
    }))
    assert.match(cfOut, /未认证/)
    assert.match(cfOut, /<thead>[\s\S]*?已配置域名[\s\S]*?<\/thead>/)
    assert.match(cfOut, /<td colspan="5" class="provider-state-cell" role="status">登录后/)

    assert.doesNotMatch(cfOut, /cached-cf-id|>缺失</)
    assert.match(cfOut, /<button[^>]*disabled[^>]*>[\s\S]*?\+ 新建 ID\s*<\/button>/)

    const menuSource = readFileSync(new URL('../src/components/common/ProviderRowActions.vue', import.meta.url), 'utf8')
    const selectFn = menuSource.slice(menuSource.indexOf('function onSelect'))
    assert.match(selectFn, /if \(!item \|\| item\.disabled\) return[\s\S]*?hide\?\.\(\)[\s\S]*?emit\('select'/)
    assert.match(menuSource, /onBeforeUnmount\(\(\) => \{[\s\S]*?instance\?\.dispose\?\.\(\)/)
    assert.match(menuSource, /strategy: 'fixed'/)
    const menuHtml = await renderToString(appFor(
      (await server.ssrLoadModule('/src/components/common/ProviderRowActions.vue')).default,
      {
        menuLabel: '更多操作',
        primary: { label: '编辑', icon: 'bi-pencil', disabled: false },
        items: [
          { key: 'clone', label: '克隆配置' },
          { key: 'delete', label: '删除', danger: true, disabled: true, separatorBefore: true },
        ],
      },
    ))
    assert.match(menuHtml, /aria-label="编辑"/)
    assert.match(menuHtml, /data-bs-toggle="tooltip"/)
    assert.match(menuHtml, /aria-label="更多操作"/)
    assert.match(menuHtml, /data-bs-toggle="dropdown"/)
    const more = menuHtml.match(/<button[^>]*aria-label="更多操作"[^>]*>/)?.[0] || ''
    assert.doesNotMatch(more, /tooltip/)
    assert.match(menuHtml, /<button(?=[^>]*disabled)(?=[^>]*text-danger)[^>]*>\s*删除\s*<\/button>/)
  } finally {
    await server.close()
  }
})
