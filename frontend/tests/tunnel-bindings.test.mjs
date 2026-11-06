import assert from 'node:assert/strict'
import { test } from 'node:test'
import { devTunnelServiceOwner, cloudflareHostnameOwner } from '../src/utils/tunnel-bindings.js'
import { devTunnelPortURLs } from '../src/utils/public-urls.js'

const tunnels = [
  { id: 1, name: 'first', provider: 'devtunnel', localHost: 'ip1.internal', localPort: 8080, devTunnel: { tunnelId: 'abc.usw3', protocol: 'http' } },
  { id: 2, name: 'cf', provider: 'cloudflare', cloudflare: { tunnelId: 'cf-id', hostname: 'dev.example.com' } },
  { id: 3, name: 'root', provider: 'cloudflare', cloudflare: { tunnelId: 'cf-root', hostname: 'example.com' } },
]

test('Dev Tunnel IDs are exclusive regardless of host, port or protocol', () => {
  for (const protocol of ['http', 'https', 'auto']) {
    const records = [{ ...tunnels[0], devTunnel: { ...tunnels[0].devTunnel, protocol } }]
    assert.equal(devTunnelServiceOwner(records, 'ABC', null)?.id, 1)
  }
  assert.equal(devTunnelServiceOwner(tunnels, 'abc.usw3', null)?.id, 1)
  assert.equal(devTunnelServiceOwner(tunnels, 'other.usw3', null), null)
  assert.equal(devTunnelServiceOwner(tunnels, 'abc.usw2', null), null)
  assert.equal(devTunnelServiceOwner(tunnels, 'abc.usw3', 1), null)
  assert.equal(devTunnelServiceOwner(tunnels, '', null), null)
})

test('Cloudflare checks complete hostname including root domains and ignores case and scheme', () => {
  assert.equal(cloudflareHostnameOwner(tunnels, 'HTTPS://DEV.EXAMPLE.COM/', null)?.id, 2)
  assert.equal(cloudflareHostnameOwner(tunnels, 'example.com', null)?.id, 3)
  assert.equal(cloudflareHostnameOwner(tunnels, 'dev.other.com', null), null)
  assert.equal(cloudflareHostnameOwner(tunnels, 'dev.example.com', 2), null)
})

test('shared ID public addresses are filtered by public port, not local target port', () => {
  const urls = ['https://abc-8080.usw3.devtunnels.ms', 'https://abc.usw3.devtunnels.ms:8080', 'https://abc-9090.usw3.devtunnels.ms']
  assert.deepEqual(devTunnelPortURLs(urls, 8080), urls.slice(0, 2))
  assert.deepEqual(devTunnelPortURLs(urls, 9090), [urls[2]])
  assert.deepEqual(devTunnelPortURLs(urls, 3000), [])
})
