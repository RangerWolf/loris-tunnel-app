import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { test } from 'node:test'
import { computed, ref } from 'vue'
import { parse } from 'vue/compiler-sfc'

const source = readFileSync(new URL('../src/components/common/CLIUpdateDialog.vue', import.meta.url), 'utf8')
function setup() {
  const events = []
  const c = vm.createContext({
    ref, computed, nextTick: callback => callback?.(), onMounted: () => {}, onBeforeUnmount: () => {},
    defineProps: () => ({ provider: 'cloudflare' }), defineEmits: () => (...args) => events.push(args),
    useI18n: () => ({ t: key => key }), window: { setInterval: () => 1, clearInterval: () => {} },
    getCLIMaintenance: async () => ({ phase: 'idle' }), cancelCLIUpdate: async () => {},
  })
  vm.runInContext(parse(source).descriptor.scriptSetup.content.replace(/^import .*$/gm, ''), c)
  return { c, events, get: expression => vm.runInContext(expression, c), set: value => { c.value = value; vm.runInContext('status.value = value', c) } }
}

test('maintenance requires an explicit usable candidate and blocks unsafe actions', () => {
  const s = setup()
  const ready = { installed: true, source: 'managed', candidateId: 'one', hasUpdate: true, comparisonKnown: true, phase: 'checked' }
  s.set(ready)
  assert.equal(s.get('canApply.value'), true)
  for (const override of [{ busy: true }, { blockedReason: 'running' }, { hasUpdate: false }, { candidateId: '' }]) {
    s.set({ ...ready, ...override })
    assert.equal(s.get('canApply.value'), false)
  }
  s.set({ ...ready, comparisonKnown: false, hasUpdate: false })
  assert.equal(s.get('phaseText.value'), 'app.cliMaintenance.cannotCompare')
  s.set({ ...ready, source: 'system' })
  assert.equal(s.get('primaryLabel.value'), 'app.cliMaintenance.switchManaged')
  s.set({ ...ready, hasUpdate: false, candidateKind: 'import' })
  assert.equal(s.get('canApply.value'), true)
  assert.equal(s.get('primaryLabel.value'), 'app.cliMaintenance.replace')
  s.set({ phase: 'updated', previousVersion: '' })
  assert.equal(s.get('phaseText.value'), 'app.cliMaintenance.phases.installed')
})

test('replacement cannot close and failed actions never emit successful refresh', async () => {
  const s = setup()
  s.set({ phase: 'replacing', busy: true })
  await s.c.close()
  assert.equal(s.events.length, 0)
  s.c.getCLIMaintenance = async () => ({ phase: 'error', error: 'write failed' })
  await s.c.run(async () => { throw new Error('write failed') })
  assert.equal(s.get('error.value'), 'write failed')
  assert.equal(s.events.length, 0)
  s.c.getCLIMaintenance = async () => ({ phase: 'restored' })
  await s.c.run(async () => ({}))
  assert.equal(s.events.at(-1)[0], 'changed')
})

test('a late progress poll cannot overwrite the final replacement result', async () => {
  const s = setup()
  let finishPoll, calls = 0
  s.c.getCLIMaintenance = () => ++calls === 1 ? new Promise(resolve => { finishPoll = resolve }) : Promise.resolve({ phase: 'updated', currentVersion: '2.0.0' })
  const polling = s.c.poll()
  await s.c.run(async () => ({ phase: 'updated' }))
  finishPoll({ phase: 'downloading', currentVersion: '1.0.0' })
  await polling
  assert.equal(s.get('status.value.phase'), 'updated')
  assert.equal(s.get('status.value.currentVersion'), '2.0.0')
  assert.equal(s.events.at(-1)[0], 'changed')
})

test('a stale cancellable UI cannot close a replacement already begun by the backend', async () => {
  const s = setup()
  s.set({ phase: 'downloading', busy: true, canCancel: true })
  s.c.cancelCLIUpdate = async () => false
  s.c.getCLIMaintenance = async () => ({ phase: 'replacing', busy: true, canCancel: false })
  await s.c.close()
  assert.equal(s.events.length, 0)
  assert.equal(s.get('status.value.phase'), 'replacing')
})
