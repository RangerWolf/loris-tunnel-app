import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { test } from 'node:test'

test('forced domain refresh bypasses cache and exposes loading and error states', async () => {
  const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
  const start = source.indexOf('async function loadCloudflareZones(')
  const end = source.indexOf('async function loadCloudflareResources(', start)
  const zones = { state: 'ready', items: [{ id: 'old' }], detail: '' }
  let release
  const context = vm.createContext({
    cloudflareZones: zones,
    cloudflareZoneRequest: null,
    cloudflareZoneSeq: 0,
    cloudflareZoneCacheAt: Date.now(),
    cloudflareCacheTTL: 300000,
    errorMessage: err => err.message,
    ListCloudflareZones: force => {
      assert.equal(force, true)
      return new Promise(resolve => { release = resolve })
    },
  })
  vm.runInContext(source.slice(start, end), context)
  const request = vm.runInContext('loadCloudflareZones(true)', context)
  assert.equal(zones.state, 'loading')
  release({ state: 'ready', items: [{ id: 'new' }], detail: '' })
  await request
  assert.equal(zones.state, 'ready')
  assert.equal(zones.items[0].id, 'new')
  context.ListCloudflareZones = async () => { throw new Error('network failed') }
  await vm.runInContext('loadCloudflareZones(true)', context)
  assert.equal(zones.state, 'error')
  assert.equal(zones.detail, 'network failed')
  assert.equal(context.cloudflareZoneRequest, null)
})

test('an old domain response cannot undo a newer authorization change', async () => {
  const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
  const start = source.indexOf('async function fetchCloudflareZones(')
  const end = source.indexOf('async function loadCloudflareResources(', start)
  const pending = []
  const c = vm.createContext({
    cloudflareZoneSeq: 0,
    cloudflareZones: { state: 'idle', items: [], detail: '' },
    cloudflareZoneCacheAt: 0,
    errorMessage: err => err.message,
    ListCloudflareZones: () => new Promise(resolve => pending.push(resolve)),
  })
  vm.runInContext(source.slice(start, end), c)
  const old = c.fetchCloudflareZones(false)
  const current = c.fetchCloudflareZones(true)
  pending[1]({ state: 'ready', items: [{ id: 'new' }] })
  await current
  pending[0]({ state: 'ready', items: [{ id: 'old' }] })
  await old
  assert.equal(c.cloudflareZones.items[0].id, 'new')
})
