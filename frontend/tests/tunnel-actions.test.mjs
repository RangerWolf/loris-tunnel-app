import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import vm from 'node:vm'
import { reactive } from 'vue'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function setup(items, pro = true) {
  const request = deferred()
  const poll = deferred()
  const calls = []
  const dialogs = []
  const context = vm.createContext({
    tunnelActionRevision: 0, pendingTunnelActions: reactive(new Map()), tunnels: { value: items },
    proLicense: { isPro: pro }, FREE_PLAN_RUNNING_LIMIT: 3,
    activeSlots: { value: 0 }, activeSlotLimit: { value: 3 },
    configMessage: { value: '' }, tunnelErrorAiDebugStates: {},
    initialStateLoading: { value: true },
    normalizeTunnelFromBackend: (item) => item,
    jumpers: { value: [] }, tunnelGroups: { value: [] },
    GetState: () => poll.promise,
    ToggleTunnel: (id) => { calls.push(id); return request.promise },
    patchTunnelLocal: (id, updated) => {
      context.tunnels.value = context.tunnels.value.map((item) => item.id === id ? { ...item, ...updated } : item)
    },
    t: (key) => key, openActionDialog: (options) => { dialogs.push(options) }, openProUpgrade: async () => {},
    logEvent: () => {}, trackTunnelAction: () => {}, errorMessage: (err) => err.message
  })
  for (const [start, end] of [
    ['async function toggleTunnel(', '\nfunction openActionDialog('],
    ['async function loadStateFromBackend(', '\nfunction syncStateSilently(']
  ]) {
    vm.runInContext(source.slice(source.indexOf(start), source.indexOf(end, source.indexOf(start))), context)
  }
  return { context, request, poll, calls, dialogs }
}

for (const status of ['running', 'stopped', 'error']) {
  test(`${status}: immediate feedback, duplicate guard, successful completion`, async () => {
    const tunnel = { id: 1, name: 'test', status, lastError: 'previous', latencyMs: 200 }
    const { context: c, request, calls } = setup([tunnel])
    const done = c.toggleTunnel(tunnel)
    assert.equal(c.pendingTunnelActions.get(1), status === 'running' ? 'stop' : 'start')
    assert.equal(c.tunnels.value[0].status, 'busy')
    await c.toggleTunnel(tunnel)
    assert.equal(calls.length, 1)
    request.resolve({ ...tunnel, status: status === 'running' ? 'stopped' : 'running', lastError: '' })
    await done
    assert.equal(c.pendingTunnelActions.size, 0)
    assert.equal(c.tunnels.value[0].status, status === 'running' ? 'stopped' : 'running')
  })
  test(`${status}: failure preserves original state and clears feedback`, async () => {
    const tunnel = { id: 1, name: 'test', status, lastError: 'previous', latencyMs: 200 }
    const { context: c, request } = setup([tunnel])
    const done = c.toggleTunnel(tunnel)
    request.reject(new Error('unavailable'))
    await done
    assert.equal(c.pendingTunnelActions.size, 0)
    assert.deepEqual(c.tunnels.value[0], tunnel)
  })
}

test('polling preserves pending state and cannot overwrite a completed action', async () => {
  const tunnel = { id: 1, name: 'test', status: 'running' }
  const { context: c, request, poll } = setup([tunnel])
  const done = c.toggleTunnel(tunnel)
  const syncing = c.loadStateFromBackend()
  request.resolve({ ...tunnel, status: 'stopped' })
  await done
  poll.resolve({ tunnels: [tunnel] })
  await syncing
  assert.equal(c.tunnels.value[0].status, 'stopped')
  assert.equal(c.pendingTunnelActions.size, 0)
})

test('stopping occupies its running slot without counting as a pending start', async () => {
  const items = [1, 2].map((id) => ({ id, name: `t${id}`, status: 'running' }))
  const target = { id: 3, name: 't3', status: 'stopped' }
  const { context: c, request, calls } = setup([...items, target], false)
  c.activeSlots.value = items.length
  const stopping = c.toggleTunnel(items[0])
  const starting = c.toggleTunnel(target)
  assert.deepEqual(calls, [1, 3])
  await c.toggleTunnel({ id: 4, status: 'stopped' })
  assert.deepEqual(calls, [1, 3])
  request.reject(new Error('test complete'))
  await Promise.all([stopping, starting])
})

test('polling during a request keeps its busy status and loading record', async () => {
  const tunnel = { id: 1, name: 'test', status: 'running' }
  const { context: c, request, poll } = setup([tunnel])
  const done = c.toggleTunnel(tunnel)
  const syncing = c.loadStateFromBackend()
  poll.resolve({ tunnels: [{ ...tunnel, status: 'stopped' }] })
  await syncing
  assert.equal(c.tunnels.value[0].status, 'busy')
  assert.equal(c.pendingTunnelActions.get(1), 'stop')
  request.resolve({ ...tunnel, status: 'stopped' })
  await done
})

test('free users are blocked at three active slots', async () => {
  const target = { id: 4, name: 't4', status: 'stopped' }
  const { context: c, calls, dialogs } = setup([target], false)
  c.activeSlots.value = 3
  await c.toggleTunnel(target)
  assert.equal(calls.length, 0)
  assert.equal(dialogs.length, 1)
  assert.equal(dialogs[0].mode, 'confirm')
  assert.equal(dialogs[0].message, 'config.messages.freePlanRunningLimit')
  assert.equal(typeof dialogs[0].onConfirm, 'function')
})

test('Pro can start a fourth tunnel even with an old three-slot snapshot', async () => {
  const target = { id: 4, name: 't4', status: 'stopped' }
  const { context: c, calls, dialogs, request } = setup([target], true)
  c.activeSlots.value = 3
  const done = c.toggleTunnel(target)
  assert.deepEqual(calls, [4])
  assert.equal(dialogs.length, 0)
  request.resolve({ ...target, status: 'running' })
  await done
})

test('poll begun before a toggle cannot overwrite its completed result', async () => {
  const tunnel = { id: 1, name: 'test', status: 'running' }
  const { context: c, request, poll } = setup([tunnel])
  const syncing = c.loadStateFromBackend()
  const done = c.toggleTunnel(tunnel)
  request.resolve({ ...tunnel, status: 'stopped' })
  await done
  poll.resolve({ tunnels: [tunnel] })
  await syncing
  assert.equal(c.tunnels.value[0].status, 'stopped')
})
