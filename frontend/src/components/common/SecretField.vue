<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import CopyIconButton from './CopyIconButton.vue'
import IconActionButton from './IconActionButton.vue'

const props = defineProps({
  value: { type: String, default: '' },
  revealable: { type: Boolean, default: true },
  disabled: { type: Boolean, default: false },
  copyAction: { type: Function, default: null },
})
const emit = defineEmits(['error'])
const { t } = useI18n()
const visible = ref(false)
const hide = () => { visible.value = false }
onMounted(() => window.addEventListener('blur', hide))
onBeforeUnmount(() => { window.removeEventListener('blur', hide) })
</script>

<template>
  <span class="secret-field">
    <span class="lt-mono secret-value">{{ visible ? value : '••••••••••••' }}</span>
    <span class="secret-actions">
      <IconActionButton v-if="revealable" button-class="lt-icon-btn" :icon-class="visible ? 'bi-eye-slash' : 'bi-eye'" :title="t(visible ? 'secret.hide' : 'secret.show')" :aria-label="t(visible ? 'secret.hide' : 'secret.show')" :disabled="disabled" @click="visible = !visible" />
      <CopyIconButton button-class="lt-icon-btn" :value="value" :copy-action="copyAction" :disabled="disabled" @error="emit('error', $event)" />
    </span>
  </span>
</template>

<style scoped>
.secret-field { display: inline-flex; align-items: center; gap: 10px; max-width: 100%; }
.secret-value { overflow-wrap: anywhere; min-width: 0; }
.secret-actions { display: inline-flex; gap: 6px; flex-shrink: 0; }
</style>
