import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'
import { parse } from 'vue/compiler-sfc'
import { ref } from 'vue'

test('secret field reuses copy feedback and remasks on blur', () => {
  const source = readFileSync(new URL('../src/components/common/SecretField.vue', import.meta.url), 'utf8')
  const { descriptor } = parse(source)
  let blur, cleanup
  const field = runInNewContext(descriptor.scriptSetup.content.replace(/^import .*$/gm, '') + '\n;({ visible })', {
    ref, defineProps: () => ({}), defineEmits: () => () => {}, useI18n: () => ({ t: key => key }),
    onMounted: fn => fn(), onBeforeUnmount: fn => { cleanup = fn },
    window: { addEventListener: (_, fn) => { blur = fn }, removeEventListener: (_, fn) => { assert.equal(fn, blur); blur = null } },
  })
  assert.match(descriptor.template.content, /<CopyIconButton[^>]*:value="value"[^>]*:copy-action="copyAction"/)
  field.visible.value = true
  blur()
  assert.equal(field.visible.value, false)
  cleanup()
  assert.equal(blur, null)
})
