import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { test } from 'node:test'
import { computed, ref, watch } from 'vue'
import { parse } from 'vue/compiler-sfc'
import { splitHostname } from '../src/utils/cloudflare.js'

const source = readFileSync(new URL('../src/components/providers/CloudflareProvider.vue', import.meta.url), 'utf8')
function setup({ remove = async () => {}, zones = [] } = {}) {
  const events = []
  const props = { status: { installed: true, accountState: 'signed_in' }, busy: '', zones: { state: 'ready', items: zones }, resources: { state: 'ready', items: [] }, tunnels: [] }
  const c = vm.createContext({
    computed, ref, watch, nextTick: callback => callback(), onBeforeUnmount: () => {},
    defineProps: () => props, defineEmits: () => (...args) => events.push(args),
    useI18n: () => ({ t: key => key }),
    normalizeHostname: value => String(value || '').toLowerCase(),
    removeCloudflareAuthorization: remove,
  })
  const script = parse(source).descriptor.scriptSetup.content.replace(/^import .*$/gm, '')
  vm.runInContext(script, c)
  return { c, props, events, get: expression => vm.runInContext(expression, c) }
}

test('domain management searches full list, checks bindings and preserves explicit reauthorization target', () => {
  const s = setup({ zones: [{ id: 'a', name: 'a.example', linkedTunnels: 1 }, { id: 'b', name: 'b.example', pendingCleanup: true }, { id: 'c', name: 'a-long-domain.example' }] })
  s.c.openDomains()
  assert.equal(s.get('domainsOpen.value'), true)
  assert.equal(s.events[0][0], 'refresh-zones')
  vm.runInContext("domainQuery.value = 'LONG'", s.c)
  assert.equal(s.get('domainRows.value.length'), 1)
  assert.equal(s.get('domainRows.value[0].id'), 'c')
  assert.equal(s.c.domainBlocked(s.props.zones.items[0]), true)
  assert.equal(s.c.domainBlocked(s.props.zones.items[1]), true)
  assert.equal(Boolean(s.c.domainBlocked(s.props.zones.items[2])), false)
  s.c.reauthorizeDomain(s.props.zones.items[2])
  assert.equal(s.get('domainsOpen.value'), false)
  assert.deepEqual(s.events.at(-1), ['login', 'c'])
})

test('removal refreshes authorization state only after backend success and preserves errors on failure', async () => {
  const calls = []
  const s = setup({ zones: [{ id: 'a', name: 'a.example' }], remove: async id => calls.push(id) })
  vm.runInContext('domainRemoveTarget.value = props.zones.items[0]', s.c)
  await s.c.confirmRemoveDomain()
  assert.deepEqual(calls, ['a'])
  assert.equal(s.get('domainRemoveTarget.value'), null)
  assert.equal(s.events.at(-1)[0], 'authorizations-changed')
  const failed = setup({ zones: [{ id: 'a', name: 'a.example' }], remove: async () => { throw new Error('cloudflare_authorization_linked') } })
  vm.runInContext('domainRemoveTarget.value = props.zones.items[0]', failed.c)
  await failed.c.confirmRemoveDomain()
  assert.equal(failed.get('domainRemoveTarget.value.id'), 'a')
  assert.equal(failed.get('domainError.value'), 'app.cloudflare.authorizationErrors.linked')
  assert.equal(failed.events.length, 0)
})

test('subdomain form always chooses the longest matching zone', () => {
  const zones = [{ name: 'example.com' }, { name: 'sub.example.com' }]
  assert.deepEqual(splitHostname('dev.sub.example.com', zones), { label: 'dev', zone: 'sub.example.com' })
  assert.deepEqual(splitHostname('sub.example.com', zones), { label: '', zone: 'sub.example.com' })
  assert.equal(splitHostname('dev.notexample.com', zones).label, 'dev.notexample.com')
})

test('login polling waits for the authorization to finish even when the existing account is connected', async () => {
  const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
  const begin = app.indexOf('async function finishCloudflareLogin(')
  const end = app.indexOf('async function cloudflareAuthorizationsChanged(', begin)
  let changes = 0, stopped = 0
  const c = vm.createContext({
    cloudflareStatus: { value: { accountState: 'signed_in', loginInProgress: true } },
    cloudflareBusy: { value: 'login:cloudflare' },
    refreshCloudflareStatus: async () => {},
    stopCloudflareLoginPoll: () => { stopped++ },
    cloudflareAuthorizationsChanged: async () => { changes++ },
  })
  vm.runInContext(app.slice(begin, end), c)
  await c.finishCloudflareLogin()
  assert.equal(c.cloudflareBusy.value, 'login:cloudflare')
  assert.equal(changes, 0)
  assert.equal(stopped, 0)
  c.cloudflareStatus.value.loginInProgress = false
  await c.finishCloudflareLogin()
  assert.equal(c.cloudflareBusy.value, '')
  assert.equal(changes, 1)
})
