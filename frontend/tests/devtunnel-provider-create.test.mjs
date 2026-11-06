import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import vm from 'node:vm'
import { isValidDevTunnelName, normalizeDevTunnelName } from '../src/utils/devtunnel-name.js'
import { devTunnelNameOwner, sameTunnelID } from '../src/utils/tunnel-bindings.js'

const source = readFileSync(new URL('../src/components/providers/DevTunnelProvider.vue', import.meta.url), 'utf8')

function slice(startMarker, endMarker) {
  const start = source.indexOf(startMarker)
  const end = source.indexOf(endMarker, start)
  if (start < 0 || end < 0) throw new Error(`cannot slice ${startMarker}`)
  return source.slice(start, end)
}

function setup({ name = '', tunnels = [], resources = [] } = {}) {
  const emitted = []
  const context = vm.createContext({
    ref: value => ({ value }),
    computed: fn => ({ get value() { return fn() } }),
    t: key => key,
    props: { tunnels, resources: { state: 'ready', detail: '', items: resources } },
    emit: (event, payload) => emitted.push([event, payload]),
    isValidDevTunnelName,
    normalizeDevTunnelName,
    devTunnelNameOwner,
    sameTunnelID,
  })
  const state = slice('const createOpen = ref(', 'const deleteTarget = ref(')
  const submit = slice('function submitCreate()', 'onMounted(')
  const exportApi = '\nglobalThis.__api = { createName, createError, createNameValue, createNameError }\n'
  vm.runInContext(state + '\n' + submit + exportApi, context)
  context.__api.createName.value = name
  context.submitCreate()
  return { emitted, api: context.__api }
}

test('provider create dialog sends an empty tunnel name for random ids', () => {
  const { emitted } = setup()
  assert.equal(emitted.length, 1)
  const [event, payload] = emitted[0]
  assert.equal(event, 'create-resource')
  assert.equal(payload.tunnelName, '')
  assert.equal(payload.access, 'private')
  assert.equal(payload.expiration, '30d')
  assert.equal(setup().api.createNameError.value, '')
})

test('clearing an invalid optional name restores random creation without a required error', () => {
  const { api } = setup({ name: 'bad_name' })
  assert.equal(api.createNameError.value, 'app.modals.tunnel.customIdInvalid')
  api.createName.value = '   '
  assert.equal(api.createNameError.value, '')
})

test('provider create dialog sends the normalized custom tunnel name', () => {
  const { emitted } = setup({ name: '  Web-Dev-App  ' })
  const [, payload] = emitted[0]
  assert.equal(payload.tunnelName, 'web-dev-app')
})

test('provider create dialog rejects an invalid custom tunnel name before calling the backend', () => {
  const { emitted, api } = setup({ name: 'my_project' })
  assert.equal(emitted.length, 0)
  assert.equal(api.createError.value, 'app.modals.tunnel.customIdInvalid')
})

test('provider create dialog blocks names already bound by another local service', () => {
  const tunnels = [{ id: 2, name: 'other', provider: 'devtunnel', devTunnel: { tunnelId: 'taken-id.usw3' } }]
  const { emitted, api } = setup({ name: 'taken-id', tunnels })
  assert.equal(emitted.length, 0)
  assert.equal(api.createError.value, 'app.modals.tunnel.customIdOccupied')
})

test('provider create dialog blocks names that already exist in the remote account', () => {
  const resources = [{ id: 'remote-id.usw3', ports: [] }]
  const { emitted, api } = setup({ name: 'remote-id', resources })
  assert.equal(emitted.length, 0)
  assert.equal(api.createError.value, 'app.modals.tunnel.customIdRemoteConflict')
})
