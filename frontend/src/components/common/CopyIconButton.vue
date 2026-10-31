<script setup>
import { ref, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import IconActionButton from './IconActionButton.vue'
import { writeTextToClipboard } from '../../utils/clipboard'

const props = defineProps({
  value: { type: String, default: '' },
  title: { type: String, default: '' },
  buttonClass: { type: String, default: 'copy-icon-btn-inline' },
  disabled: { type: Boolean, default: false },
  copyAction: { type: Function, default: null },
})
const emit = defineEmits(['error', 'copied'])
const { t } = useI18n()
const copied = ref(false)
let timer
let revision = 0
function reset() {
  revision++
  clearTimeout(timer)
  copied.value = false
}
watch(() => props.value, reset)
onBeforeUnmount(reset)
async function copy() {
  const current = revision
  try {
    const ok = props.copyAction ? await props.copyAction() : await writeTextToClipboard(props.value)
    if (!ok) throw new Error(t('cloudSync.copyFailed'))
    if (current !== revision) return
    copied.value = true
    emit('copied')
    clearTimeout(timer)
    timer = setTimeout(() => { copied.value = false }, 1500)
  } catch (error) {
    if (current === revision) emit('error', String(error))
  }
}
</script>

<template>
  <IconActionButton
    class="copy-icon-btn"
    :class="{ 'is-copied': copied }"
    :button-class="buttonClass"
    :icon-class="copied ? 'bi-check2' : 'bi-copy'"
    :title="copied ? t('secret.copied') : title || t('secret.copy')"
    :aria-label="copied ? t('secret.copied') : title || t('secret.copy')"
    :disabled="disabled"
    @click="copy"
  />
</template>
