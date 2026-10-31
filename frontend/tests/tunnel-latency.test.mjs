import assert from 'node:assert/strict'
import { test } from 'node:test'
import { latencyLabel, latencyTitle } from '../src/utils/tunnel-latency.js'
const t = key => key
const format = ms => `${ms} ms`
test('latency states cover all providers and never display stale failed measurements', () => {
  for (const provider of ['ssh', 'devtunnel', 'cloudflare']) {
    const tunnel = { provider, status: 'running', latencyMs: 42, latencyState: 'ok' }
    assert.equal(latencyLabel(tunnel, t, format), '42 ms')
    assert.equal(latencyLabel({ ...tunnel, status: 'stopped' }, t, format), '—')
    if (provider === 'ssh') continue
    for (const [state, label] of Object.entries({ failed: 'failed', timeout: 'timeout', auth_required: 'authRequired', restricted: 'restricted', measuring: 'measuring' })) {
      assert.equal(latencyLabel({ ...tunnel, latencyState: state }, t, format), `app.tunnels.latency.${label}`)
    }
  }
})
test('tooltip explains protocol-specific semantics, status and measurement time', () => {
  const title = latencyTitle({ provider: 'cloudflare', status: 'running', latencyHttpStatus: 503, latencyCheckedAt: '2026-10-21T05:00:00Z', latencyDetail: 'Public request timed out' }, t)
  assert.match(title, /publicHint/)
  assert.match(title, /HTTP 503/)
  assert.match(title, /checkedAt/)
  assert.match(title, /timed out/)
  assert.match(latencyTitle({ provider: 'ssh' }, t), /sshHint/)
})

test('FRP TCP uses connection timing hint while HTTP retains response timing', () => {
  assert.match(latencyTitle({ provider: 'frp', mode: 'tcp' }, t), /tcpHint/)
  assert.match(latencyTitle({ provider: 'frp', mode: 'http' }, t), /publicHint/)
})
