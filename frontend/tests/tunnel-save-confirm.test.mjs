import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import vm from 'node:vm'
import { parse } from 'vue/compiler-sfc'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const css = readFileSync(new URL('../src/styles/app-shell.css', import.meta.url), 'utf8')

function setup(provider = 'devtunnel', access = 'public') {
  let saves = 0
  const c = vm.createContext({
    tunnelForm: { provider, access }, actionDialog: { visible: false },
    showTunnelModal: { value: true }, t: key => key,
    persistTunnel: async () => {
      assert.equal(c.actionDialog.visible, false, 'confirmation closes before saving')
      saves++
      c.showTunnelModal.value = false
    },
  })
  for (const [start, end] of [
    ['async function saveTunnel()', 'async function persistTunnel('],
    ['function openActionDialog(', 'function openRedeemDialog('],
  ]) vm.runInContext(source.slice(source.indexOf(start), source.indexOf(end, source.indexOf(start))), c)
  return { c, saves: () => saves }
}

test('saving public, private and Cloudflare tunnels submits directly without a second confirmation', async () => {
  for (const [provider, access] of [['devtunnel', 'public'], ['devtunnel', 'private'], ['cloudflare', 'public']]) {
    const { c, saves } = setup(provider, access)
    await c.saveTunnel()
    assert.equal(c.actionDialog.visible, false)
    assert.equal(saves(), 1)
  }
})

test('public access warning remains visible in the tunnel form', () => {
  const form = readFileSync(new URL('../src/components/tunnel-forms/DevTunnelForm.vue', import.meta.url), 'utf8')
  assert.match(form, /field-warning[\s\S]*app\.modals\.tunnel\.publicWarning/)
})

test('shared confirmations escape ancestor stacking contexts and sit above editor and provider dialogs', () => {
  const template = parse(source).descriptor.template.content
  assert.match(template, /<Teleport to="body">\s*<div v-if="actionDialog.visible" class="overlay action-confirm-overlay">/)
  assert.match(template, /role="dialog" aria-modal="true" aria-labelledby="action-confirm-title" aria-describedby="action-confirm-message"/)
  const rule = css.match(/\.action-confirm-overlay\s*\{([^}]+)\}/)[1]
  assert.match(rule, /position:\s*fixed/)
  const zIndex = selector => Number(css.match(new RegExp(`\\.${selector}\\s*\\{[^}]*z-index:\\s*(\\d+)`))[1])
  assert.ok(zIndex('action-confirm-overlay') > zIndex('overlay'))
  assert.ok(zIndex('action-confirm-overlay') > zIndex('devtunnel-resource-overlay'))
})
