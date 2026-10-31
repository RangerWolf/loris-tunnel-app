import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import { parse } from 'vue/compiler-sfc'
import { computed, ref } from 'vue'

const source = readFileSync(new URL('../src/components/common/CloudSyncCard.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
function fixture(api = {}) {
  let mounted
  const card = runInNewContext(descriptor.scriptSetup.content.replace(/^import .*$/gm, '') +
    '\n;({ receive, setEnabled, state, loaded, loading, loadError, busy, toggling, token, showToken, reconnecting, message, messageIsError, generate, copyToken })', {
    ref, computed, defineEmits: () => () => {}, useI18n: () => ({ t: key => key, locale: ref('en') }),
    onMounted: fn => { mounted = fn }, onBeforeUnmount: () => {},
    window: { addEventListener() {} }, EventsOn: () => () => {}, api,
  })
  return { card, mount: () => mounted() }
}

test('initial loading and failed status reads never expose a usable toggle or connection', async () => {
  let reject
  const { card, mount } = fixture({ GetCloudSyncStatus: () => new Promise((_, fail) => { reject = fail }) })
  const ready = mount()
  assert.equal(card.loaded.value, false)
  assert.equal(card.loading.value, true)
  reject(new Error('invalid local cloud sync settings'))
  await ready
  assert.equal(card.loaded.value, false)
  assert.equal(card.loading.value, false)
  assert.match(card.loadError.value, /invalid local/)
  const event = { target: { checked: true } }
  await card.setEnabled(event)
  assert.equal(event.target.checked, false)
  assert.equal(card.state.value.enabled, false)
})

test('toggle waits for backend, rejects duplicates and retains state on failure', async () => {
  let resolve, reject, calls = 0
  const { card } = fixture({ SetCloudSyncEnabled: () => { calls++; return new Promise((yes, no) => { resolve = yes; reject = no }) } })
  card.receive({ enabled: false, connected: true, state: 'disabled' })
  const event = { target: { checked: true } }
  const pending = card.setEnabled(event)
  assert.equal(event.target.checked, false)
  assert.equal(card.state.value.enabled, false)
  assert.equal(card.busy.value, true)
  assert.equal(card.toggling.value, true)
  await card.setEnabled({ target: { checked: true } })
  assert.equal(calls, 1)
  resolve({ enabled: true, connected: true, state: 'pending', automatic: true })
  await pending
  assert.equal(card.state.value.enabled, true)
  assert.equal(card.busy.value, false)
  assert.equal(card.toggling.value, null)
  const disable = { target: { checked: false } }
  const failed = card.setEnabled(disable)
  assert.equal(disable.target.checked, true)
  reject(new Error('settings are read-only'))
  await failed
  assert.equal(card.state.value.enabled, true)
  assert.match(card.message.value, /read-only/)
  assert.equal(card.messageIsError.value, true)
  assert.equal(card.busy.value, false)
})

test('successful disabling clears drafts, reconnect mode and old feedback without deleting credentials', async () => {
  let copies = 0
  const { card } = fixture({
    SetCloudSyncEnabled: async () => ({ enabled: false, connected: true, state: 'disabled', automatic: true }),
    CopyCloudSyncToken: async () => { copies++ },
  })
  card.receive({ enabled: true, connected: true, state: 'error' })
  card.token.value = 'draft-secret'
  card.showToken.value = true
  card.reconnecting.value = true
  card.message.value = 'old error'
  await card.setEnabled({ target: { checked: false } })
  assert.equal(card.state.value.connected, true)
  assert.equal(card.state.value.automatic, true)
  assert.equal(card.token.value, '')
  assert.equal(card.showToken.value, false)
  assert.equal(card.reconnecting.value, false)
  assert.equal(card.message.value, '')
  await card.copyToken()
  assert.equal(copies, 0)
})

test('closed UI gates the existing controls and global banner, retains navigation and translates states', () => {
  const template = descriptor.template.content
  assert.match(template, /v-if="loading"/)
  assert.match(template, /v-else-if="loadError"/)
  assert.match(template, /v-else-if="!state.enabled"/)
  assert.match(template, /v-if="loaded && state.enabled && state.connected && !reconnecting"/)
  assert.match(template, /id="sync-enabled"[^>]*:disabled="!loaded \|\| busy"/)
  assert.match(template, /<label for="sync-enabled"/)
  assert.match(template, /aria-describedby="sync-enabled-hint"/)
  assert.match(descriptor.styles[0].content, /\.sync-header-actions[^}]*flex-wrap: wrap/)
  const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
  assert.match(app, /cloudSyncStatus\?\.enabled &&\s*\['update_available', 'conflict', 'error'\]/)
  assert.match(app, /class="sync-banner"/)
  assert.match(app, /switchPage\('sync'\)/)
  assert.doesNotMatch(app, /alert-warning/)
  assert.match(app, /key: 'sync', title: t\('app.sidebar.sync'\)/)
  const config = readFileSync(new URL('../src/components/pages/ConfigPage.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(config, /sync-enabled|SetCloudSyncEnabled/)
  for (const lang of ['en', 'ru', 'zh-CN', 'zh-HK', 'zh-TW']) {
    const messages = JSON.parse(readFileSync(new URL(`../src/locales/${lang}.json`, import.meta.url), 'utf8')).cloudSync
    for (const key of ['enabled', 'loading', 'enabling', 'disabling', 'disabledHint', 'disabledDescription', 'connectHint', 'review']) assert.ok(messages[key], `${lang}: ${key}`)
    assert.ok(messages.states.disabled)
    for (const key of ['error', 'conflict', 'update_available', 'version']) assert.ok(messages.banner[key], `${lang}: banner.${key}`)
  }
})
