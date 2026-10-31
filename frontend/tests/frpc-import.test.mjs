import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import vm from 'node:vm'
import { computed, ref, watch } from 'vue'
import { parse } from 'vue/compiler-sfc'

const source = readFileSync(new URL('../src/utils/frpc-import.js', import.meta.url), 'utf8')
const { importFRPC, frpImportSignature } = await import('data:text/javascript;base64,' + Buffer.from(source.replace(/^import .*$/gm, '')).toString('base64'))
const row = (values = {}) => ({ name: 'original-proxy-name-longer-than-display', displayName: 'Display name', type: 'tcp', localIP: '127.0.0.1', localPort: 22, remotePort: 6000, domain: '', errors: [], selected: true, ...values })
function setup(rows, overrides = {}) {
  const created = [], resolved = [], deleted = []
  const api = {
    async ResolveFRPCImportServer(...args) { resolved.push(args); return { id: 'server' } },
    async CreateTunnel(payload) { created.push(payload) },
    async DeleteFRPServer(id) { deleted.push(id) },
    ...overrides,
  }
  return { created, resolved, deleted, run: (options = {}) => importFRPC({ content: 'source', format: 'toml', existingID: '', name: 'New server', rows, existingTunnels: [], ...options }, api) }
}

test('FRPC imports independent proxy names, same local port with different remote ports, and stopped defaults', async () => {
  const rows = [row(), row({ name: 'second', remotePort: 6001 })]
  const s = setup(rows)
  const result = await s.run()
  assert.equal(result.imported, 2)
  assert.equal(s.resolved.length, 1)
  assert.equal(s.created[0].name, 'Display name')
  assert.equal(s.created[0].frp.proxyName, rows[0].name)
  for (const payload of s.created) {
    assert.equal(payload.frp.serverId, 'server')
    assert.equal(payload.status, 'stopped')
    assert.equal(payload.autoStart, false)
    assert.equal(JSON.stringify(payload).includes('token'), false)
  }
  assert.notEqual(frpImportSignature(s.created[0]), frpImportSignature(s.created[1]))
  assert.equal(rows.every(r => r.imported && !r.selected), true)
})

test('FRPC skips existing duplicates without creating or resolving a server', async () => {
  const first = setup([row()])
  await first.run()
  const s = setup([row()])
  const result = await s.run({ existingID: 'server', existingTunnels: first.created })
  assert.equal(result.skipped, 1)
  assert.equal(s.resolved.length, 0)
  assert.equal(s.created.length, 0)
})

test('FRPC ignores invalid and deselected rows without creating an orphan server', async () => {
  const s = setup([row({ selected: false }), row({ errors: ['unsupported'] })])
  assert.equal((await s.run()).imported, 0)
  assert.equal(s.resolved.length, 0)
})

test('FRPC continues after a row fails and retries only failed rows', async () => {
  const rows = [row(), row({ name: 'second', remotePort: 6001 })]
  let fail = true
  const s = setup(rows, { async CreateTunnel(payload) { if (fail && payload.frp.remotePort === 6000) throw new Error('public endpoint conflict') } })
  let result = await s.run()
  assert.equal(result.imported, 1)
  assert.equal(result.failed, 1)
  assert.equal(rows[0].saveError, 'public endpoint conflict')
  assert.equal(rows[1].imported, true)
  assert.deepEqual(s.deleted, [])
  fail = false
  result = await s.run({ existingID: 'server' })
  assert.equal(result.imported, 1)
})

test('FRPC removes only a newly created server when every row fails', async () => {
  const fail = { async CreateTunnel() { throw new Error('limit') } }
  const s = setup([row()], fail)
  assert.equal((await s.run()).failed, 1)
  assert.deepEqual(s.deleted, ['server'])
  const existing = setup([row()], fail)
  await existing.run({ existingID: 'server' })
  assert.deepEqual(existing.deleted, [])
  const cleanup = setup([row()], { ...fail, async DeleteFRPServer() { throw new Error('cleanup') } })
  assert.equal((await cleanup.run()).cleanupFailed, true)
})

function panel(overrides = {}) {
  const result = { format: 'toml', server: { host: 'example.com', port: 7000 }, matchedServerIds: [], errors: [], warnings: [], proxies: [row()] }
  const context = vm.createContext({
    computed, ref, watch, Promise, props: { existingTunnels: [] }, emit() {}, defineExpose() {},
    t: key => key, frpState: { servers: [] }, async refreshFRP() {},
    async ParseFRPCImport() { return result }, async importFRPC() { return { imported: 1 } },
    ...overrides,
  })
  const script = parse(readFileSync(new URL('../src/components/modals/FRPCImportPanel.vue', import.meta.url), 'utf8')).descriptor.scriptSetup.content
    .replace(/^import .*$/gm, '')
    .replace(/^const props = defineProps.*$/m, '')
    .replace(/^const emit = defineEmits.*$/m, '')
    .replace(/^const \{ t \} = useI18n\(\).*$/m, '')
  vm.runInContext(script, context)
  return { context, get: expression => vm.runInContext(expression, context), set: statement => vm.runInContext(statement, context) }
}

test('FRPC preview selects only eligible rows and removing the last selection disables import', async () => {
  const s = panel()
  await s.context.parse()
  s.set("rows.value.push({ ...rows.value[0], errors: ['invalid'], selected: false }, { ...rows.value[0], imported: true, selected: false })")
  s.context.selectAll(false)
  assert.equal(s.get('selected.value.length'), 0)
  s.context.selectAll(true)
  assert.equal(s.get('selected.value.length'), 1)
  assert.equal(s.get('allSelected.value'), true)
  s.set('busy.value = true')
  s.context.removeRow(0)
  assert.equal(s.get('rows.value.length'), 3)
  s.set('busy.value = false')
  s.context.removeRow(0)
  assert.equal(s.get('rows.value.length'), 2)
  assert.equal(s.get('canImport.value'), false)
})

test('FRPC panel reads files, parses and invalidates preview when source changes', async () => {
  const s = panel()
  const event = { target: { files: [{ name: 'frpc.YML', size: 32, text: async () => 'serverAddr: example.com' }], value: 'file' } }
  await s.context.readFile(event)
  assert.equal(s.get('content.value'), 'serverAddr: example.com')
  assert.equal(s.get('format.value'), 'yml')
  assert.equal(event.target.value, '')
  await s.context.parse()
  assert.equal(s.get('rows.value.length'), 1)
  assert.equal(s.get('canImport.value'), true)
  assert.equal(s.get('rows.value[0].displayName.length'), 20)
  s.set("content.value = 'changed'")
  assert.equal(s.get('preview.value'), null)
  assert.equal(s.get('canImport.value'), null)
})

test('FRPC panel automatically selects matching server and blocks unsupported client settings', async () => {
  const s = panel({ frpState: { servers: [{ id: 'existing', name: 'Local' }] }, async ParseFRPCImport() {
    return { format: 'json', server: { host: 'example.com', port: 7000 }, matchedServerIds: ['existing'], errors: ['unsupported transport'], proxies: [row()] }
  } })
  await s.context.parse()
  assert.equal(s.get('mode.value'), 'existing')
  assert.equal(s.get('canImport.value'), false)
})

test('FRPC panel rejects oversized files and does not expose parser source in errors', async () => {
  const s = panel({ async ParseFRPCImport() { throw new Error('secret-token') } })
  await s.context.readFile({ target: { files: [{ size: 2 * 1024 * 1024 + 1 }], value: '' } })
  assert.equal(s.get('error.value'), 'app.frpImport.fileError')
  await s.context.parse()
  assert.equal(s.get('error.value'), 'app.frpImport.parseError')
  assert.equal(s.get('busy.value'), false)
})

test('FRPC closes only after successful import and clearing busy state; partial failures stay open', async () => {
  for (const failed of [0, 1]) {
    const events = []
    let refreshed = false
    const s = panel({
      emit: (...args) => {
        if (args[0] === 'imported' || args[0] === 'complete') assert.equal(refreshed, true)
        events.push(args)
      },
      async refreshFRP() { refreshed = true },
      async importFRPC() { return { imported: 1, skipped: 0, failed, cleanupFailed: false } },
    })
    await s.context.parse()
    refreshed = false
    events.length = 0
    await s.context.save()
    assert.equal(events.some(([name]) => name === 'imported'), true)
    assert.equal(events.some(([name]) => name === 'complete'), failed === 0)
    if (!failed) assert.deepEqual(events.slice(-2), [['busy', false], ['complete']])
    assert.equal(s.get('busy.value'), false)
  }
})
