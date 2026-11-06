import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { test } from 'node:test'
import { isDevTunnelNameConflict } from '../src/utils/devtunnel-name.js'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
function setup(result) {
  let finishList
  const context = vm.createContext({
    devTunnelResourceBusy: { value: '' }, devTunnelCreated: { value: null },
    devTunnelCacheTTL: 60000, devTunnelListRequest: null, devTunnelDetailCache: new Map(),
    devTunnelCreateError: { value: '' }, devTunnelResourceCacheAt: 0, devTunnelResourceSeq: 0,
    devTunnelResources: { state: 'idle', items: [{ id: 'existing.usw3' }], detail: '' },
    CreateDevTunnelResource: async () => result,
    regionCreateMessage: (detail) => detail,
    ListDevTunnelResources: () => new Promise((resolve) => { finishList = resolve }),
    t: (key) => key, errorMessage: (err) => err.message,
    isDevTunnelNameConflict,
  })
  for (const [start, end] of [
    ['async function loadDevTunnelResources(', '\nfunction applyRemoteExpiration('],
    ['async function createDevTunnelResource(', '\nasync function deleteDevTunnelResourceByID('],
  ]) {
    vm.runInContext(source.slice(source.indexOf(start), source.indexOf(end, source.indexOf(start))), context)
  }
  return { context, finishList: (result) => finishList(result) }
}

test('created ID is available immediately and an older list cannot remove it', async () => {
  const resource = { id: 'new.usw3', ports: [], access: 'private' }
  const { context: c, finishList } = setup({ state: 'ready', resource })
  const oldRefresh = c.loadDevTunnelResources(true)
  await c.createDevTunnelResource({})
  assert.equal(c.devTunnelResourceBusy.value, '')
  assert.equal(c.devTunnelCreated.value.id, resource.id)
  assert.deepEqual(Array.from(c.devTunnelResources.items, (item) => item.id), ['new.usw3', 'existing.usw3'])
  finishList({ state: 'ready', items: [] })
  await oldRefresh
  assert.equal(c.devTunnelResources.items[0].id, resource.id)
})

test('failed creation does not add an ID or show success', async () => {
  const { context: c } = setup({ state: 'error', detail: 'creation failed' })
  await c.createDevTunnelResource({})
  assert.equal(c.devTunnelCreated.value, null)
  assert.equal(c.devTunnelResources.items.length, 1)
  assert.equal(c.devTunnelCreateError.value, 'creation failed')
})

test('a name conflict is reported with the localized conflict message', async () => {
  const { context: c } = setup({ state: 'error', detail: 'dev tunnel name is already in use' })
  await c.createDevTunnelResource({})
  assert.equal(c.devTunnelCreateError.value, 'app.modals.tunnel.customIdRemoteConflict')
})

test('concurrent list requests share one query and a fresh result is reused', async () => {
  const { context: c, finishList } = setup({})
  const first = c.loadDevTunnelResources()
  const request = c.devTunnelListRequest
  const second = c.loadDevTunnelResources()
  assert.equal(c.devTunnelListRequest, request)
  finishList({ state: 'ready', items: [{ id: 'cached.usw3' }] })
  await Promise.all([first, second])
  await c.loadDevTunnelResources()
  assert.equal(c.devTunnelListRequest, null)
  assert.equal(c.devTunnelResources.items[0].id, 'cached.usw3')
})

test('details share in-flight queries, reuse cache and ignore a previous dialog request', async () => {
  const pending = new Map()
  let calls = 0
  const c = vm.createContext({
    devTunnelDetailCache: new Map(), devTunnelCacheTTL: 60000, devTunnelCacheEpoch: 0,
    devTunnelDetailSeq: 0, devTunnelDetail: { value: null },
    GetDevTunnelResource: (id) => { calls++; return new Promise(resolve => pending.set(id, resolve)) },
    errorMessage: err => err.message,
  })
  const start = source.indexOf('function getDevTunnelResourceCached(')
  vm.runInContext(source.slice(start, source.indexOf('\nfunction openNewTunnelFromDevTunnelID', start)), c)
  const first = c.inspectDevTunnelResource('a')
  const shared = c.getDevTunnelResourceCached('a')
  assert.equal(calls, 1)
  const second = c.inspectDevTunnelResource('b')
  pending.get('b')({ state: 'ready', resource: { id: 'b' } })
  await second
  pending.get('a')({ state: 'ready', resource: { id: 'a' } })
  await Promise.all([first, shared])
  assert.equal(c.devTunnelDetail.value.resource.id, 'b')
  await c.inspectDevTunnelResource('a')
  assert.equal(calls, 2)
  assert.equal(c.devTunnelDetail.value.resource.id, 'a')
})


test('normal list reads use disk cache and explicit refresh bypasses it', async () => {
  const { context: c } = setup({})
  const calls = []
  c.ListDevTunnelResources = async force => {
    calls.push(force)
    return { state: 'ready', items: [] }
  }
  await c.loadDevTunnelResources(false)
  await c.loadDevTunnelResources(true)
  assert.deepEqual(calls, [false, true])
})

test('Cloudflare forwards refresh intent to both resources and zones', async () => {
  const calls = []
  const c = vm.createContext({
    cloudflareZoneRequest: null, cloudflareZoneSeq: 0, cloudflareListRequest: null,
    cloudflareZones: { state: 'idle', items: [] }, cloudflareZoneCacheAt: 0,
    cloudflareResources: { state: 'idle', items: [] }, cloudflareResourceCacheAt: 0,
    cloudflareResourceSeq: 0, cloudflareResourceBusy: { value: '' },
    cloudflareDetailCache: new Map(), cloudflareCacheTTL: 60000,
    ListCloudflareResources: async force => { calls.push(['resources', force]); return { state: 'ready', items: [] } },
    ListCloudflareZones: async force => { calls.push(['zones', force]); return { state: 'ready', items: [] } },
    errorMessage: err => err.message,
  })
  const start = source.indexOf('async function loadCloudflareZones(')
  vm.runInContext(source.slice(start, source.indexOf('function getCloudflareResourceCached(', start)), c)
  await c.loadCloudflareResources(false)
  await c.loadCloudflareZones(false)
  await c.loadCloudflareResources(true)
  if (c.cloudflareZoneRequest) await c.cloudflareZoneRequest
  assert.deepEqual(calls, [['resources', false], ['zones', false], ['zones', true], ['resources', true]])
})

test('manual refresh waits for an in-flight cache read then performs a live query', async () => {
  const { context: c, finishList } = setup({})
  const initial = c.loadDevTunnelResources(false)
  const refresh = c.loadDevTunnelResources(true)
  const calls = []
  c.ListDevTunnelResources = async force => { calls.push(force); return { state: 'ready', items: [] } }
  finishList({ state: 'ready', items: [] })
  await Promise.all([initial, refresh])
  assert.deepEqual(calls, [true])
})
