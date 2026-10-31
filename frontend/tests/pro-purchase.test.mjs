import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { createContext, runInContext } from 'node:vm'

const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
function functionSource(name) {
  const start = app.search(new RegExp(`(?:async )?function ${name}\\(`))
  const end = app.indexOf('\n}\n', start) + 2
  assert.ok(start >= 0 && end > start)
  return app.slice(start, end)
}
function setup(overrides = {}) {
  const ctx = createContext({
    licenseStateRevision: 0,
    redeemDialog: { visible: true, code: ' TEST-CODE ', error: '', submitting: false },
    proLicense: { isPro: false },
    configMessage: { value: '' },
    proExpiryLabel: { value: '2027-10-22' },
    LIFETIME_DURATION_DAYS: 36500,
    t: key => key,
    logEvent() {},
    errorMessage: err => err.message,
    GetLicenseStatusAPI: async () => ({ active: true }),
    RedeemLicenseCodeAPI: async () => ({ success: true, active: true, code: 'TEST-CODE' }),
    ...overrides,
  })
  for (const name of ['applyLicenseState', 'refreshLicenseStatus', 'openRedeemDialog', 'closeRedeemDialog', 'submitRedeemDialog']) {
    runInContext(functionSource(name), ctx)
  }
  return ctx
}

test('activation blocks duplicate requests and closes only after active success', async () => {
  let calls = 0, resolve
  const ctx = setup({ RedeemLicenseCodeAPI: code => {
    assert.equal(code, 'TEST-CODE')
    calls++
    return new Promise(done => { resolve = done })
  } })
  const pending = ctx.submitRedeemDialog()
  ctx.closeRedeemDialog()
  assert.equal(ctx.redeemDialog.visible, true)
  await ctx.submitRedeemDialog()
  assert.equal(calls, 1)
  resolve({ success: true, active: true, expire_time: '2027-10-22', code: 'TEST-CODE' })
  await pending
  assert.equal(ctx.redeemDialog.visible, false)
  assert.equal(ctx.proLicense.isPro, true)
})

test('invalid, expired and unavailable responses keep the code and dialog for retry', async () => {
  for (const response of [null, { success: false, message: 'Invalid code' }, { success: true, active: false, message: 'Already redeemed' }, new Error('Network unavailable')]) {
    const ctx = setup({ RedeemLicenseCodeAPI: async () => {
      if (response instanceof Error) throw response
      return response
    } })
    await ctx.submitRedeemDialog()
    assert.equal(ctx.redeemDialog.visible, true)
    assert.equal(ctx.redeemDialog.code, ' TEST-CODE ')
    assert.equal(ctx.proLicense.isPro, false)
    assert.equal(ctx.redeemDialog.submitting, false)
    assert.ok(ctx.redeemDialog.error)
    if (response?.success) assert.equal(ctx.redeemDialog.error, 'purchase.inactiveCode')
  }
})

test('late refresh cannot overwrite a newly activated license; failures retain trusted state', async () => {
  let resolve
  const ctx = setup({ GetLicenseStatusAPI: () => new Promise(done => { resolve = done }) })
  const pending = ctx.refreshLicenseStatus()
  ctx.licenseStateRevision++
  ctx.proLicense.isPro = true
  resolve({ active: false })
  assert.equal(await pending, null)
  assert.equal(ctx.proLicense.isPro, true)
  for (const response of [null, new Error('Network unavailable')]) {
    ctx.GetLicenseStatusAPI = async () => {
      if (response instanceof Error) throw response
      return response
    }
    assert.equal(await ctx.refreshLicenseStatus(), false)
    assert.equal(ctx.proLicense.isPro, true)
  }
})

test('purchase component has the correct products and complete localized copy', () => {
  const source = readFileSync(new URL('../src/components/common/ProPurchaseDialog.vue', import.meta.url), 'utf8')
  assert.match(source, /id: 'monthly', price: '1.90', url: 'https:\/\/www.16688.com.cn\/goods\/G95265345'/)
  assert.match(source, /id: 'yearly', price: '19.99', url: 'https:\/\/www.16688.com.cn\/goods\/G53380769'/)
  assert.match(source, /<dialog ref="dialog" autofocus tabindex="-1"/)
  assert.match(source, /el\.focus\(\{ preventScroll: true \}\)/)
  let keys
  for (const locale of ['zh-CN', 'zh-TW', 'zh-HK', 'en', 'ru']) {
    const data = JSON.parse(readFileSync(new URL(`../src/locales/${locale}.json`, import.meta.url)))
    keys ??= Object.keys(data.purchase).sort()
    assert.deepEqual(Object.keys(data.purchase).sort(), keys)
    assert.ok(Object.values(data.purchase).every(value => typeof value === 'string' && value.length))
  }
})
