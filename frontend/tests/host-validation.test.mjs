import assert from 'node:assert/strict'
import { test } from 'node:test'
import { isValidTargetHost } from '../src/utils/host-validation.js'

test('accepts complete IP addresses and hostnames', () => {
  for (const host of ['127.0.0.1', '192.168.1.20', '::1', '2001:db8::1', 'localhost', 'app.internal', 'web-server', 'app.internal.']) {
    assert.equal(isValidTargetHost(host), true, host)
  }
})
test('rejects incomplete IPs, malformed addresses, URLs and embedded ports', () => {
  for (const host of ['', '0.0.0.0', '::', '::ffff:0.0.0.0', '1.2.3', '256.1.2.3', '01.2.3.4', '123', '1.2.3.', 'a..b', '-app.local', 'app-.local', 'https://app.local', 'host:8080', '[::1]', '2001:::1', ' host', 'a/b', 'a_b']) {
    assert.equal(isValidTargetHost(host), false, host)
  }
})
