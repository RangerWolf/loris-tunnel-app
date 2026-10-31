import assert from 'node:assert/strict'
import { devTunnelPortURLs } from '../src/utils/public-urls.js'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { test } from 'node:test'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const describe = source.slice(source.indexOf('function describeDevTunnel('), source.indexOf('\nfunction getTunnelJumperLabel('))
function setup(state = 'ready', items = [{ id: 'abc.usw3', ports: [], urls: [] }]) {
  return runInNewContext(describe + '\n;describeDevTunnel', {
    devTunnelPortURLs, devTunnelResources: { state, items }, t: key => key,
    preferDevTunnelURL: value => String(value || '').split(',').find(item => item.startsWith('https://')) || '',
    sameDevTunnelID: (a, b) => a === b,
  })
}
const tunnel = { status: 'stopped', localPort: 8080, devTunnel: { tunnelId: 'abc.usw3' } }

test('URL placeholders describe the public address, regardless of port metadata', () => {
  const describe = setup()
  assert.equal(describe(tunnel).routeBottom, 'app.tunnels.publicUrlAfterStart')
  for (const status of ['busy', 'running']) {
    assert.equal(describe({ ...tunnel, status }).routeBottom, 'app.tunnels.waitingForUrl')
  }
})

test('a live or known public address is never hidden by missing port metadata', () => {
  const url = 'https://abc-8080.usw3.devtunnels.ms/'
  for (const state of ['ready', 'network']) {
    const info = setup(state)({ ...tunnel, publicUrl: url })
    assert.equal(info.url, url)
    assert.equal(info.routeBottom, url)
  }
  const info = setup('ready', [{ id: 'abc.usw3', ports: [], urls: [url] }])(tunnel)
  assert.equal(info.routeBottom, url)
})

test('lookup problems stay in the tooltip rather than replacing the public address placeholder', () => {
  const info = setup('ready', [])(tunnel)
  assert.equal(info.routeBottom, 'app.tunnels.publicUrlAfterStart')
  assert.equal(info.routeTitle, 'app.tunnels.connection.remoteMissing')
})
