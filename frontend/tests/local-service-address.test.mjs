import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import vm from 'node:vm'
import { reactive, ref, watch } from 'vue'
import { parse } from 'vue/compiler-sfc'

const source = readFileSync(new URL('../src/components/tunnel-forms/LocalServiceAddress.vue', import.meta.url), 'utf8')
function setup(values = {}) {
  const form = reactive({ provider: 'cloudflare', localHost: '127.0.0.1', localPort: 3000, mode: 'http', ...values })
  const c = vm.createContext({ props: { tunnelForm: form }, ref, watch, URL, t: key => key })
  const script = parse(source).descriptor.scriptSetup.content
    .replace(/^import .*$/gm, '')
    .replace(/^const props = defineProps.*$/m, '')
    .replace(/^const \{ t \} = useI18n\(\).*$/m, '')
  vm.runInContext(script, c)
  return {
    form,
    address: () => vm.runInContext('address.value', c),
    input(value) {
      let error = ''
      c.updateAddress({ target: { value, setCustomValidity: message => { error = message } } })
      return error
    },
  }
}

test('service address maps HTTP, HTTPS, default ports and IPv6 into existing target fields', () => {
  const s = setup()
  assert.equal(s.address(), 'http://127.0.0.1:3000')
  assert.equal(s.input('https://localhost:8443'), '')
  assert.equal(s.form.localHost, 'localhost')
  assert.equal(s.form.localPort, 8443)
  assert.equal(s.form.mode, 'https')
  assert.equal(s.input('https://localhost'), '')
  assert.equal(s.form.localPort, 443)
  assert.equal(s.input('http://[::1]:8080'), '')
  assert.equal(s.form.localHost, '::1')
  assert.equal(s.form.localPort, 8080)
})

test('FRP mode follows the local service scheme', () => {
  const s = setup({ provider: 'frp', mode: 'tcp' })
  assert.equal(s.address(), 'tcp://127.0.0.1:3000')
  assert.equal(s.input('http://127.0.0.1:8080'), '')
  assert.equal(s.form.mode, 'http')
  assert.equal(s.form.localPort, 8080)
  assert.equal(s.input('tcp://127.0.0.1:3000'), '')
  assert.equal(s.form.mode, 'tcp')
  assert.equal(s.input('https://127.0.0.1:443'), 'app.frp.localAddressInvalid')
})

test('Dev Tunnel keeps automatic protocol and an existing port cannot be changed', () => {
  const s = setup({ provider: 'devtunnel', mode: 'auto' })
  assert.equal(s.input('https://localhost:8443'), '')
  assert.equal(s.form.mode, 'auto')
  s.form.portLocked = true
  assert.notEqual(s.input('https://localhost:8080'), '')
  assert.equal(s.form.localPort, 8443)
  assert.equal(s.input('https://localhost:8443'), '')
})

test('invalid URLs cannot silently reuse the last valid target', () => {
  for (const url of ['ftp://localhost:3000', 'http://localhost:3000/api', 'http://user:pass@localhost', 'http://localhost?q=1', 'http://localhost#x', 'http://localhost:70000', 'invalid', '']) {
    const s = setup()
    assert.notEqual(s.input(url), '', url)
    assert.equal(s.form.localHost, url, 'invalid input must reach target validation instead of testing an old target')
  }
})

test('selecting another existing ID updates the visible service port', () => {
  const s = setup()
  s.input('http://localhost:3000')
  s.form.localPort = 8080
  assert.equal(s.address(), 'http://localhost:8080')
})
