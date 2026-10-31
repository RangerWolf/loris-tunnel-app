import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { test } from 'node:test'

const source = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
for (const [name, prefix] of [['DevTunnel', 'devTunnel'], ['Cloudflare', 'cloudflare']]) {
  for (const failed of [true, false]) {
    test(`${name} installation ${failed ? 'preserves failure after refresh' : 'refreshes after success'}`, async () => {
      const status = { value: {} }
      const busy = { value: '' }
      let refreshed = 0
      const context = vm.createContext({
        [`${prefix}Status`]: status,
        [`${prefix}Busy`]: busy,
        [`Install${name}CLI`]: async () => { if (failed) throw new Error('download failed') },
        [`refresh${name}Status`]: async () => { refreshed++; status.value = { installed: !failed, detail: 'checked' } },
        errorMessage: err => err.message,
      })
      const start = source.indexOf(`async function install${name}()`)
      const end = source.indexOf(`function cancel${name}Install`, start)
      await vm.runInContext(`${source.slice(start, end)}\ninstall${name}()`, context)
      assert.equal(busy.value, '')
      assert.equal(refreshed, 1)
      assert.equal(status.value.detail, failed ? 'download failed' : 'checked')
    })
  }
}
