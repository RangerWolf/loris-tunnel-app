import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
const source = readFileSync(new URL('../src/utils/clipboard.js', import.meta.url), 'utf8')
  .replace(/^import .*$/gm, '').replace('export async function', 'async function')
function load(context) { return runInNewContext(source + '\n;writeTextToClipboard', context) }

test('desktop copying prefers Wails and browser copying falls back after native failure', async () => {
  const calls = []
  let nativeOK = true
  const copy = load({ window: { runtime: { ClipboardSetText: true } },
    ClipboardSetText: async text => { calls.push(['native', text]); return nativeOK },
    navigator: { clipboard: { writeText: async text => { calls.push(['browser', text]) } } },
  })
  assert.equal(await copy('a'), true)
  assert.deepEqual(calls, [['native', 'a']])
  nativeOK = false
  assert.equal(await copy('b'), true)
  assert.deepEqual(calls.slice(1), [['native', 'b'], ['browser', 'b']])
})

test('browser fallback always removes temporary input and restores focus, even on failure', async () => {
  const calls = []
  const input = { style: {}, setAttribute() {}, select() {}, remove: () => calls.push('remove') }
  const copy = load({ document: {
    activeElement: { focus: () => calls.push('focus') }, createElement: () => input,
    body: { appendChild() {} }, execCommand: () => { throw new Error('denied') },
  } })
  assert.equal(await copy('a'), false)
  assert.deepEqual(calls, ['remove', 'focus'])
  assert.equal(await copy(''), false)
})
