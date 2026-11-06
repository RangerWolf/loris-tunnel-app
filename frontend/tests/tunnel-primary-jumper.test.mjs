import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { test } from 'node:test'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
const normalize = source.slice(source.indexOf('function normalizeJumperIdList('), source.indexOf('\nfunction toHostKey('))
const action = source.slice(source.indexOf('function setPrimaryJumperForTunnelChain('), source.indexOf('\nfunction removeJumperFromTunnelChain('))

function setup(ids) {
  const tunnelForm = { jumperIds: ids, nextJumperId: 'unchanged' }
  const candidates = []
  const select = runInNewContext(`${normalize}\n${action}\nsetPrimaryJumperForTunnelChain`, {
    tunnelForm,
    getNextTunnelJumperCandidate(ids) {
      candidates.push(Array.from(ids))
      return 99
    },
  })
  return { tunnelForm, candidates, select }
}

for (const [name, before, selected, expected] of [
  ['replaces a single hop', [1], 2, [2]],
  ['keeps a repeated selection single', [1], 1, [1]],
  ['initializes an empty chain', [], 2, [2]],
  ['moves an existing hop to the front', [1, 2, 3], 2, [2, 1, 3]],
  ['prepends a new hop to a multi-hop chain', [1, 2], 3, [3, 1, 2]],
  ['normalizes IDs before deciding whether to preserve hops', ['1', 1], '2', [2]],
]) {
  test(name, () => {
    const c = setup(before)
    c.select(selected)
    assert.deepEqual(Array.from(c.tunnelForm.jumperIds), expected)
    assert.deepEqual(c.candidates, [expected])
    assert.equal(c.tunnelForm.nextJumperId, 99)
  })
}

test('successive primary changes do not accumulate hops', () => {
  const c = setup([1])
  for (const id of [2, 3, 1]) {
    c.select(id)
    assert.deepEqual(Array.from(c.tunnelForm.jumperIds), [id])
  }
})

test('invalid selections leave the chain and candidate untouched', () => {
  const c = setup([1])
  for (const id of [0, -1, 1.5, '', null, undefined, 'invalid']) c.select(id)
  assert.deepEqual(c.tunnelForm.jumperIds, [1])
  assert.equal(c.tunnelForm.nextJumperId, 'unchanged')
  assert.deepEqual(c.candidates, [])
})
