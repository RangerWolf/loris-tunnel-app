import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import { parse } from 'vue/compiler-sfc'
import { computed, ref, withModifiers } from 'vue'

test('visibility icon forwards the click event and toggles Token in both directions', () => {
  const source = readFileSync(new URL('../src/components/common/IconActionButton.vue', import.meta.url), 'utf8')
  const { descriptor } = parse(source)
  const clickExpression = descriptor.template.content.match(/@click="([^"]+)"/)[1]
  const visible = ref(true)
  let stopped = 0
  const event = { stopPropagation: () => { stopped++ } }
  const handler = withModifiers(() => { visible.value = !visible.value }, ['stop'])
  const click = () => runInNewContext(clickExpression, {
    $event: event,
    $emit: (name, forwardedEvent) => {
      assert.equal(name, 'click')
      assert.equal(forwardedEvent, event)
      handler(forwardedEvent)
    },
  })
  click()
  assert.equal(visible.value, false)
  click()
  assert.equal(visible.value, true)
  assert.equal(stopped, 2)
})

test('sync has its own navigation entry, page and review destination', () => {
  const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
  const app = read('../src/App.vue')
  const settings = read('../src/components/pages/ConfigPage.vue')
  const page = read('../src/components/pages/SyncPage.vue')
  assert.match(app, /key: 'sync', title: t\('app.sidebar.sync'\)/)
  assert.ok(app.indexOf("key: 'sync'") < app.indexOf("key: 'logs'"))
  assert.match(app, /<SyncPage\s+v-if="activePage === 'sync'"\s+@reload-state="loadStateFromBackend"\s+@confirm-action="openActionDialog"/)
  assert.match(app, /\['update_available', 'conflict', 'error'\]\.includes\(cloudSyncStatus\.state\) &&\s*activePage === 'overview'/)
  assert.match(app, /@click="switchPage\('sync'\)"/)
  assert.doesNotMatch(settings, /CloudSyncCard/)
  assert.match(page, /@confirm-action="\$emit\('confirm-action', \$event\)"/)
  assert.match(page, /@reload-state="\$emit\('reload-state'\)"/)
  const card = read('../src/components/common/CloudSyncCard.vue')
  assert.match(card, /class="cloud-sync-card small"/)
  assert.doesNotMatch(page + card, /cloud-sync-card mt-3|class="mt-0"/)
  assert.match(card, /<CardHeader :title="t\('cloudSync.title'\)"/)
  const summary = parse(card).descriptor.template.content.match(/<dl class="lt-kv sync-summary">([\s\S]*?)<\/dl>/)?.[1]
  assert.ok(summary, 'Cloud Sync key-value list should exist')
  assert.match(summary, /t\('cloudSync.lastUpdate'\)[\s\S]*t\('cloudSync.automatic'\)[\s\S]*id="sync-auto"/)
  assert.match(summary, /id="sync-auto-hint"/)
  assert.match(summary, /@change="preferences\(\$event.target.checked\)"/)
  assert.equal((card.match(/id="sync-auto"/g) || []).length, 1)
  assert.doesNotMatch(card, /class="lt-card sync-automatic"/)
  assert.match(card, /class="form-control form-control-sm"/)
  assert.match(card, /class="btn btn-sm btn-outline-secondary"[^>]*@click="generate"/)
  for (const lang of ['en', 'ru', 'zh-CN', 'zh-HK', 'zh-TW']) {
    const messages = JSON.parse(read(`../src/locales/${lang}.json`))
    assert.ok(messages.app.sidebar.sync)
    assert.ok(messages.app.sidebar.syncSubtitle)
  }
})

test('connected Device & Server keeps the status inline and actions in the value column', () => {
  const source = readFileSync(new URL('../src/components/common/CloudSyncCard.vue', import.meta.url), 'utf8')
  const connection = parse(source).descriptor.template.content.match(/<dl class="lt-kv sync-connection">([\s\S]*?)<\/dl>/)?.[1]
  assert.ok(connection)
  const deviceIndex = connection.indexOf("t('cloudSync.deviceName')")
  const serverIndex = connection.indexOf("t('cloudSync.server')")
  const tokenIndex = connection.indexOf("t('cloudSync.token')")
  const secretIndex = connection.indexOf('<SecretField')
  const actionsIndex = connection.indexOf('class="sync-manage-actions"')
  assert.ok(deviceIndex < serverIndex && serverIndex < tokenIndex && tokenIndex < secretIndex && secretIndex < actionsIndex)
  assert.match(connection, /class="d-flex align-items-center flex-wrap gap-2"[\s\S]*?<span class="lt-pill lt-pill--success"/)
  assert.doesNotMatch(connection, /<small><span class="lt-pill/)
  assert.match(connection, /class="btn btn-sm btn-link text-danger text-decoration-none"[^>]*@click="disconnect"/)
  assert.doesNotMatch(source, /sync-disconnect/)
})

test('generated Token is visible and can be copied before connecting', async () => {
  const source = readFileSync(new URL('../src/components/common/CloudSyncCard.vue', import.meta.url), 'utf8')
  const { descriptor } = parse(source)
  let clipboard = '', clipboardOK = true, savedCopies = 0
  let status = { enabled: true, connected: false, server_url: 'http://127.0.0.1:8000', device_name: 'Office' }
  const card = runInNewContext(
    descriptor.scriptSetup.content.replace(/^import .*$/gm, '') +
    '\n;({ receive, generate, copyToken, connect, test, updateToken, tokenValid, tokenInvalid, tokenFeedback, feedbackIsError, cancelReconnect, token, showToken, reconnecting, message })',
    {
      setTimeout, clearTimeout, computed, ref, defineEmits: () => () => {}, useI18n: () => ({ t: key => key, locale: ref('en') }),
      onMounted: () => {}, onBeforeUnmount: () => {},
      ClipboardSetText: async text => { clipboard = text; return clipboardOK },
      api: {
        GenerateCloudSyncToken: async (...args) => { assert.equal(args.length, 0); return 'a'.repeat(43) },
        GetCloudSyncStatus: async () => status,
        ConnectCloudSync: async token => {
          assert.equal(token, 'a'.repeat(43))
          status = { ...status, connected: true }
        },
        CopyCloudSyncToken: async () => { savedCopies++ },
      },
    },
  )
  card.receive(status)
  assert.equal(card.tokenValid.value, false)
  assert.equal(card.tokenInvalid.value, false, 'empty input should not show an error immediately')
  assert.equal(card.tokenFeedback.value, 'cloudSync.tokenHint', 'show usage instructions before any action')
  assert.equal(card.feedbackIsError.value, false)
  for (const value of ['  ' + 'a'.repeat(43) + '  ', 'a'.repeat(21) + ' \t\r\n\u00a0\u3000' + 'a'.repeat(22)]) {
    const event = { target: { value } }
    card.message.value = 'old feedback'
    card.updateToken(event)
    assert.equal(event.target.value, 'a'.repeat(43), 'clean whitespace from the visible input')
    assert.equal(card.token.value, 'a'.repeat(43), 'validate and submit the cleaned Token')
    assert.equal(card.tokenValid.value, true)
    assert.equal(card.message.value, '')
  }
  card.updateToken({ target: { value: ' \t\u3000' } })
  assert.equal(card.token.value, '')
  assert.equal(card.tokenInvalid.value, false)
  card.updateToken({ target: { value: ' a!中_- ' } })
  assert.equal(card.token.value, 'a!中_-', 'keep non-whitespace characters for validation')
  assert.equal(card.tokenInvalid.value, true)
  for (const invalid of ['中文' + 'a'.repeat(41), 'a'.repeat(42), 'a'.repeat(44), 'a'.repeat(42) + '!', 'a'.repeat(21) + ' ' + 'a'.repeat(21)]) {
    card.token.value = invalid
    assert.equal(card.tokenValid.value, false)
    assert.equal(card.tokenInvalid.value, true)
    await card.connect()
    await card.test()
    assert.equal(card.token.value, invalid, 'validation must not rewrite the input')
  }
  card.token.value = '  ' + 'a'.repeat(43) + '  '
  assert.equal(card.tokenValid.value, true, 'match the existing trim-on-connect behavior')
  await card.generate()
  assert.equal(card.tokenValid.value, true)
  assert.equal(card.tokenInvalid.value, false)
  assert.equal(card.showToken.value, true)
  assert.equal(card.token.value, 'a'.repeat(43))
  assert.equal(card.tokenFeedback.value, 'cloudSync.generated')
  assert.equal(card.feedbackIsError.value, false)
  card.token.value = '中文'
  assert.equal(card.tokenFeedback.value, 'cloudSync.tokenInvalid', 'error must replace the generated hint in the shared slot')
  assert.equal(card.feedbackIsError.value, true)
  card.updateToken({ target: { value: '  ' + 'a'.repeat(21) + '\t' + 'a'.repeat(22) + ' ' } })
  await card.copyToken()
  assert.equal(clipboard, 'a'.repeat(43))
  assert.equal(savedCopies, 0)
  assert.equal(card.message.value, 'cloudSync.copied')
  assert.equal(card.tokenFeedback.value, 'cloudSync.copied')
  assert.equal(card.feedbackIsError.value, false)
  clipboardOK = false
  await card.copyToken()
  assert.match(card.message.value, /cloudSync.copyFailed/)
  assert.equal(card.feedbackIsError.value, true)
  await card.connect()
  assert.equal(card.token.value, '')
  assert.equal(card.showToken.value, false)
  await card.copyToken()
  assert.equal(savedCopies, 1)
  card.reconnecting.value = true
  clipboardOK = true
  await card.generate()
  await card.copyToken()
  assert.equal(savedCopies, 1, 'reconnect must copy the new draft, not the saved Token')
  card.cancelReconnect()
  assert.equal(card.token.value, '')
  assert.equal(card.showToken.value, false)
  card.message.value = ''
  assert.equal(card.tokenFeedback.value, 'cloudSync.tokenHint', 'restore the default hint when feedback clears')
  assert.match(descriptor.template.content, /:type="showToken \? 'text' : 'password'"/)
  assert.match(descriptor.template.content, /:value="token"/)
  assert.match(descriptor.template.content, /@input="updateToken"/)
  assert.match(descriptor.template.content, /@click.stop="showToken = !showToken"/)
  assert.match(descriptor.template.content, /class="sync-token-input"/)
  assert.ok(descriptor.template.content.indexOf("t('cloudSync.deviceName')") < descriptor.template.content.indexOf('for="sync-token"'), 'readonly device name should precede Token')
  assert.match(descriptor.styles[0].content, /\.sync-device \{ display: flex; flex-direction: column; gap: 0\.25rem; \}/)
  assert.match(descriptor.template.content, /'is-invalid': tokenInvalid/)
  assert.match(descriptor.template.content, /v-if="tokenFeedback" id="sync-token-feedback"/)
  assert.match(descriptor.template.content, /:aria-describedby="tokenFeedback \? 'sync-token-feedback' : undefined"/)
  assert.match(descriptor.template.content, /v-if="loaded && \(\(!state.enabled \|\| \(state.connected && !reconnecting\)\) && \(state.error \|\| message\)\)"/)
  assert.match(descriptor.template.content, /class="sync-token-field mb-3"/)
  assert.doesNotMatch(descriptor.template.content, /sync-token-row mb-3|invalid-feedback d-block mb-3/)
  assert.match(descriptor.styles[0].content, /\.sync-token-feedback \{ margin-top: 0\.25rem; font-size: 0\.78rem; line-height: 1\.4;/)
  assert.match(descriptor.styles[0].content, /height: 2\.8em; overflow-y: auto;/)
  assert.match(descriptor.template.content, /:aria-invalid="tokenInvalid"/)
  assert.match(descriptor.template.content, /:disabled="busy \|\| !server \|\| !tokenValid" @click="test"/)
  assert.match(descriptor.template.content, /:disabled="busy \|\| !server \|\| !tokenValid \|\| !deviceName.trim\(\)" @click="connect"/)
  assert.match(descriptor.styles[0].content, /\.sync-token-input \.form-control::-ms-reveal\s*\{\s*display: none;\s*\}/)
  assert.match(descriptor.template.content, /:title="t\('cloudSync.copyToken'\)"\s+@click="copyToken"/)
  assert.doesNotMatch(descriptor.template.content.split('<template v-else>')[0], /bi-copy|\{\{ t\('cloudSync.copyToken'\) \}\}/)
  assert.doesNotMatch(descriptor.template.content, /sync-email|cloudSync.email/)
  for (const lang of ['en', 'ru', 'zh-CN', 'zh-HK', 'zh-TW']) {
    const messages = JSON.parse(readFileSync(new URL(`../src/locales/${lang}.json`, import.meta.url), 'utf8'))
    for (const key of ['showToken', 'hideToken', 'copyFailed', 'tokenInvalid']) assert.ok(messages.cloudSync[key])
  }
})
