import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import { parse } from 'vue/compiler-sfc'
import { ref } from 'vue'

const source = readFileSync(new URL('../src/components/common/CopyIconButton.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
function setup() {
  const props = { value: 'text', copyAction: null }
  let ok = true, reset, cleanup, clipboard
  const timers = new Map(), errors = []
  let nextTimer = 0
  const button = runInNewContext(descriptor.scriptSetup.content.replace(/^import .*$/gm, '') + '\n;({ copy, copied })', {
    ref, defineProps: () => props, defineEmits: () => (_, error) => errors.push(error), useI18n: () => ({ t: key => key }),
    writeTextToClipboard: async value => { clipboard = value; return ok },
    watch: (_, fn) => { reset = fn }, onBeforeUnmount: fn => { cleanup = fn },
    setTimeout: (fn, ms) => { assert.equal(ms, 1500); timers.set(++nextTimer, fn); return nextTimer },
    clearTimeout: id => timers.delete(id),
  })
  return { props, button, timers, errors, fail: () => { ok = false }, reset: () => reset(), cleanup: () => cleanup(), clipboard: () => clipboard }
}

test('copy succeeds, shows check, resets after 1.5 seconds, and repeated copies reset timer', async () => {
  const c = setup()
  await c.button.copy()
  assert.equal(c.clipboard(), 'text')
  assert.equal(c.button.copied.value, true)
  await c.button.copy()
  assert.equal(c.timers.size, 1)
  c.timers.values().next().value()
  assert.equal(c.button.copied.value, false)
  assert.match(descriptor.template.content, /copied \? 'bi-check2' : 'bi-copy'/)
})

test('failed native or custom copying never shows success', async () => {
  const c = setup()
  c.fail()
  await c.button.copy()
  assert.equal(c.button.copied.value, false)
  assert.equal(c.errors.length, 1)
  c.props.copyAction = async () => false
  await c.button.copy()
  assert.equal(c.errors.length, 2)
})

test('changing content or unmounting discards pending copy feedback and timers', async () => {
  for (const reset of ['reset', 'cleanup']) {
    const c = setup()
    let finish
    c.props.copyAction = () => new Promise(resolve => { finish = resolve })
    const pending = c.button.copy()
    c[reset]()
    finish(true)
    await pending
    assert.equal(c.button.copied.value, false)
    assert.equal(c.timers.size, 0)
  }
})
