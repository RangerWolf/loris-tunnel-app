import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { cloudflareOptionDisabled, cloudflarePublicURL, isValidHostname, joinHostname, normalizeHostname, splitHostname } from './cloudflare.js'

test('hostname normalization drops scheme and slash', () => {
  assert.equal(normalizeHostname(' HTTPS://DevSpace.Example.com/ '), 'devspace.example.com')
  assert.equal(cloudflarePublicURL('devspace.example.com'), 'https://devspace.example.com')
  assert.equal(isValidHostname('https://devspace.example.com/path'), false)
  assert.equal(isValidHostname(''), false)
})

test('authorized zone joins an optional hostname label', () => {
  const zones = [{ name: 'example.com' }]
  assert.equal(joinHostname('dev', 'example.com'), 'dev.example.com')
  assert.equal(joinHostname('', 'example.com'), 'example.com')
  assert.deepEqual(splitHostname('dev.example.com', zones), { label: 'dev', zone: 'example.com' })
  assert.deepEqual(splitHostname('example.com', zones), { label: '', zone: 'example.com' })
})

test('tunnel id options disable used and missing credentials', () => {
  assert.equal(cloudflareOptionDisabled({ credentialReady: true }, ''), false)
  assert.equal(cloudflareOptionDisabled({ credentialReady: true }, 'DevSpace'), true)
  assert.equal(cloudflareOptionDisabled({ credentialReady: false }, ''), true)
})

test('cloudflare form and provider tab expose the first-version fields', () => {
  const form = readFileSync(new URL('../components/tunnel-forms/CloudflareTunnelForm.vue', import.meta.url), 'utf8')
  const modal = readFileSync(new URL('../components/modals/TunnelModal.vue', import.meta.url), 'utf8')
  const providers = readFileSync(new URL('../components/pages/ProvidersPage.vue', import.meta.url), 'utf8')
  const features = readFileSync(new URL('../config/features.js', import.meta.url), 'utf8')
  for (const label of ['tunnelId', 'localHost', 'localPort', 'hostname']) {
    assert.match(form, new RegExp(label))
  }
  assert.doesNotMatch(form, /expiration|accessPrivate|remoteHost|jumper/i)
  assert.match(modal, /value="cloudflare"/)
  assert.match(providers, /selectTab\('cloudflare'\)/)
  assert.doesNotMatch(providers, /cloudflare[\s\S]{0,80}disabled/)
  assert.match(features, /cloudflare:\s*\{\s*enabled:\s*true/)
})
