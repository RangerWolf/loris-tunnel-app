import assert from 'node:assert/strict'
import { test } from 'node:test'
import { uniquePublicURLs } from '../src/utils/public-urls.js'

test('deduplicates equivalent root URLs while retaining the two address forms', () => {
  const subdomain = 'https://abc-8080.usw3.devtunnels.ms'
  const port = 'https://abc.usw3.devtunnels.ms:8080'
  assert.deepEqual(uniquePublicURLs([subdomain, port, subdomain + '/', port + '/']), [subdomain, port])
})

test('preserves distinct paths, queries and ports and rejects invalid browser URLs', () => {
  const urls = ['https://example.com/a', 'https://example.com/a/', 'https://example.com/?x=1', 'https://example.com/?x=2', 'https://example.com:8080/']
  assert.deepEqual(uniquePublicURLs([...urls, 'javascript:alert(1)', 'invalid']), urls)
})
