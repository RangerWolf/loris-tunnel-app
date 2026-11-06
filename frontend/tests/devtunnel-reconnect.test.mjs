import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import { devTunnelReconnectText } from '../src/utils/devtunnel-reconnect.js'

for (const locale of ['en', 'zh-CN', 'zh-TW', 'zh-HK', 'ru']) {
  test(`reconnect diagnostics are localized with honest peer identity (${locale})`, () => {
    const messages = JSON.parse(readFileSync(new URL(`../src/locales/${locale}.json`, import.meta.url)))
    const t = (key, params = {}) => key.split('.').reduce((o, k) => o[k], messages).replace(/\{(\w+)\}/g, (_, k) => params[k])
    const info = { kind: 'host_conflict', attempt: 3, delaySeconds: 4.1, detail: 'SSH disconnected' }
    const unknown = devTunnelReconnectText(info, t)
    assert.ok(unknown.includes(messages.app.tunnels.reconnect.unknownHost))
    assert.ok(unknown.includes('3'))
    assert.ok(unknown.includes('5'))
    const observed = devTunnelReconnectText({ ...info, hostIds: ['peer-one'] }, t)
    assert.ok(observed.includes('peer-one'))
    assert.ok(!observed.includes(messages.app.tunnels.reconnect.unknownHost))
    const network = devTunnelReconnectText({ ...info, kind: 'transient' }, t)
    assert.ok(network.includes(messages.app.tunnels.reconnect.disconnected))
    assert.ok(!network.includes(messages.app.tunnels.reconnect.unknownHost))
    assert.equal(devTunnelReconnectText(null, t), '')
  })
}
