import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { test } from 'node:test'
import { isValidTargetHost } from '../src/utils/host-validation.js'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const action = source.slice(source.indexOf('async function testTunnelConnection()'), source.indexOf('\nasync function runJumperAIDebug()'))

function setup(provider, { failure, localHost = '127.0.0.1', name = 'test' } = {}) {
  const state = {}
  const calls = []
  const payload = { provider, name, localHost, localPort: 3000 }
  const run = runInNewContext(`${action}; testTunnelConnection`, {
    tunnelForm: { provider, jumperIds: [], appendNewJumper: false },
    tunnelTest: state, tunnelValidationError: { value: '' }, tunnelAiDebug: {},
    resetInlineJumperValidation() {}, resetAIDebugState() {}, resetTunnelTest() {},
    normalizeJumperIdList: ids => ids, buildTunnelPayloadForTest: () => payload,
    isValidTargetHost, t: key => key, errorMessage: error => error.message,
    async TestTunnelConnectionAPI(...args) {
      calls.push(args)
      if (failure) throw new Error(failure)
    },
  })
  return { run, state, calls, payload }
}

for (const provider of ['cloudflare', 'devtunnel']) {
  test(`${provider} tests the local service without a jumper`, async () => {
    const c = setup(provider)
    await c.run()
    assert.equal(c.calls.length, 1)
    assert.equal(c.calls[0][0], c.payload)
    assert.equal(c.calls[0][1], null)
    assert.equal(c.state.status, 'success')
    assert.equal(c.state.message, 'app.modals.tunnel.testPassedLocal')
  })

  test(`${provider} reports backend connection failures without success or SSH debugging`, async () => {
    const c = setup(provider, { failure: 'target service is not accepting connections' })
    await c.run()
    assert.equal(c.calls.length, 1)
    assert.equal(c.state.status, 'error')
    assert.equal(c.state.message, 'target service is not accepting connections')
    assert.equal(c.state.debuggable, false)
  })

  test(`${provider} rejects missing fields and invalid local hosts before testing`, async () => {
    for (const [input, message] of [
      [{ name: '' }, 'app.modals.tunnel.testRequiredFields'],
      [{ localHost: 'https://localhost' }, 'app.modals.tunnel.invalidTargetHost'],
    ]) {
      const c = setup(provider, input)
      await c.run()
      assert.equal(c.calls.length, 0)
      assert.equal(c.state.status, 'error')
      assert.equal(c.state.message, message)
    }
  })
}

test('SSH and the default provider still require a jumper', async () => {
  for (const provider of ['ssh', undefined]) {
    const c = setup(provider)
    await c.run()
    assert.equal(c.calls.length, 0)
    assert.equal(c.state.status, 'error')
    assert.equal(c.state.message, 'app.modals.tunnel.testSelectJumper')
  }
})
