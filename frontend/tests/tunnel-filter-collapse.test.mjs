import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { test } from 'node:test'
import { ref, watch, nextTick } from 'vue'

const source = readFileSync(new URL('../src/components/pages/TunnelsPage.vue', import.meta.url), 'utf8')

test('provider and status changes preserve collapsed groups, including hidden groups', async () => {
  const c = vm.createContext({
    ref, watch, props: { searchQuery: '' }, emit() {},
    localSearchQuery: ref(''), statusFilter: ref('all'), providerFilter: ref('all'),
    collapsedSectionKeys: ref(new Set(['company', 'hidden'])),
    tunnelSections: ref([{ key: 'company', tunnels: [{ id: 1 }] }]),
  })
  const start = source.indexOf('watch(() => props.searchQuery')
  const end = source.indexOf('\nwatch(\n  () => props.tunnels', start)
  vm.runInContext(source.slice(start, end), c)
  for (const provider of ['ssh', 'devtunnel', 'all']) {
    c.providerFilter.value = provider
    await nextTick()
    assert.deepEqual([...c.collapsedSectionKeys.value], ['company', 'hidden'])
  }
  for (const status of ['running', 'error', 'all']) {
    c.statusFilter.value = status
    await nextTick()
    assert.deepEqual([...c.collapsedSectionKeys.value], ['company', 'hidden'])
  }
})
