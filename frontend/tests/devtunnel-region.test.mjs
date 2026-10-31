import assert from 'node:assert/strict'
import { test } from 'node:test'
import { devTunnelCluster, orderedRegions, regionCreateMessage, regionLabel } from '../src/utils/devtunnel-region.js'

test('regions with latency sort first and failures stay selectable', () => {
  const ordered = orderedRegions([
    { id: 'euw', status: 'timeout', latencyMs: null },
    { id: 'usw2', status: 'ok', latencyMs: 40 },
    { id: 'use', status: 'error' },
    { id: 'usw3', status: 'ok', latencyMs: 12 },
  ])
  assert.deepEqual(ordered.map((region) => region.id), ['usw3', 'usw2', 'euw', 'use'])
})

test('cluster labels and creation mismatches stay explicit', () => {
  assert.equal(devTunnelCluster('abc123.usw3'), 'usw3')
  assert.equal(devTunnelCluster('abc123'), '')
  assert.equal(regionLabel([{ id: 'usw3', name: 'West US 3' }], 'usw3'), 'West US 3 (usw3)')
  assert.equal(regionLabel([], 'eun1'), 'eun1')
  const t = (key, values) => `${key}:${values.actual}:${values.requested}`
  assert.equal(
    regionCreateMessage('created in eun1 instead of euw; the new tunnel was removed', t),
    'app.modals.tunnel.regionMismatch:eun1:euw',
  )
  assert.equal(regionCreateMessage('creation failed', t), 'creation failed')
})
