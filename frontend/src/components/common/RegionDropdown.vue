<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { orderedRegions } from '../../utils/devtunnel-region'

const props = defineProps({
  modelValue: { type: String, default: '' },
  regions: { type: Array, default: () => [] },
  state: { type: String, default: 'idle' },
  loading: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
})

const emit = defineEmits(['update:modelValue', 'test'])
const { t } = useI18n()
const labelId = `region-label-${Math.random().toString(36).slice(2, 8)}`
const root = ref(null)
const toggleEl = ref(null)
const menuEl = ref(null)
const open = ref(false)
const frozen = ref(null)
const menuStyle = ref({})

const selected = computed(() => (props.regions || []).find((region) => region.id === props.modelValue))
const currentLabel = computed(() => {
  if (!props.modelValue) return t('app.modals.tunnel.regionAuto')
  if (selected.value?.name) return `${selected.value.name} (${selected.value.id})`
  return props.modelValue
})
const testLabel = computed(() => {
  if (props.loading) return t('app.modals.tunnel.regionTesting')
  if (props.state === 'error') return t('app.modals.tunnel.regionRetry')
  return t('app.modals.tunnel.regionTest')
})
const rows = computed(() => {
  const list = props.regions || []
  if (open.value && frozen.value?.length) {
    const byID = new Map(list.map((region) => [region.id, region]))
    const current = frozen.value.map((id) => byID.get(id)).filter(Boolean)
    const seen = new Set(frozen.value)
    return [...current, ...list.filter((region) => !seen.has(region.id))]
  }
  return orderedRegions(list)
})

watch(() => props.regions, (list) => {
  if (!open.value || frozen.value?.length) return
  frozen.value = orderedRegions(list || []).map((region) => region.id)
})

function latencyText(region) {
  if (region?.status === 'ok' && Number.isFinite(region.latencyMs)) return `${region.latencyMs} ms`
  if (region?.status === 'timeout') return t('app.modals.tunnel.regionTimeout')
  if (region?.status === 'error') return t('app.modals.tunnel.regionFailed')
  return '—'
}

function placeMenu() {
  const el = toggleEl.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const gap = 4
  const below = window.innerHeight - rect.bottom - gap - 8
  const above = rect.top - gap - 8
  const upward = below < 80 && above > below
  const limit = Math.max(80, Math.min(240, upward ? above : below))
  menuStyle.value = {
    top: upward ? `${Math.max(8, rect.top - gap - limit)}px` : `${rect.bottom + gap}px`,
    left: `${Math.max(8, rect.left)}px`,
    width: `${rect.width}px`,
    maxHeight: `${limit}px`,
  }
}

function toggle() {
  if (props.disabled) return
  open.value = !open.value
  if (!open.value) return
  frozen.value = orderedRegions(props.regions || []).map((region) => region.id)
}

function onLayout() {
  if (open.value) placeMenu()
}

function choose(id) {
  emit('update:modelValue', id)
  open.value = false
}

function onPointerDown(event) {
  const target = event.target
  if (root.value?.contains(target) || menuEl.value?.contains(target)) return
  open.value = false
}

function onKeydown(event) {
  if (event.key === 'Escape') open.value = false
}

watch(open, async (value) => {
  if (!value) return
  await nextTick()
  placeMenu()
})

onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown)
  document.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', onLayout)
  window.addEventListener('scroll', onLayout, true)
  emit('test')
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onPointerDown)
  document.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', onLayout)
  window.removeEventListener('scroll', onLayout, true)
})
</script>

<template>
  <div ref="root" class="region-dropdown">
    <div class="region-field-head">
      <label class="form-label" :id="labelId">{{ t('app.modals.tunnel.region') }}</label>
      <button type="button" class="btn btn-link btn-sm region-test" :disabled="loading" @click="emit('test')">
        <i v-if="loading" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
        {{ testLabel }}
      </button>
    </div>
    <div class="region-dropdown-control">
    <button
      ref="toggleEl"
      type="button"
      class="form-select region-dropdown-toggle"
      :disabled="disabled"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-labelledby="labelId"
      @click="toggle"
    >
      {{ currentLabel }}
    </button>
    <Teleport to="body">
    <div v-if="open" ref="menuEl" class="region-dropdown-menu" role="listbox" :aria-labelledby="labelId" :style="menuStyle">
      <button
        type="button"
        class="region-dropdown-option"
        role="option"
        :aria-selected="!modelValue"
        :class="{ 'is-selected': !modelValue }"
        @click="choose('')"
      >
        <span>{{ t('app.modals.tunnel.regionAuto') }}</span>
      </button>
      <button
        v-for="region in rows"
        :key="region.id"
        type="button"
        class="region-dropdown-option"
        role="option"
        :aria-selected="modelValue === region.id"
        :class="{ 'is-selected': modelValue === region.id }"
        @click="choose(region.id)"
      >
        <span>{{ region.name }} ({{ region.id }})</span>
        <span class="region-dropdown-latency">{{ latencyText(region) }}</span>
      </button>
    </div>
    </Teleport>
    </div>
    <p class="field-note mb-0">{{ t('app.modals.tunnel.regionHint') }}</p>
    <p v-if="state === 'error' && !loading" class="field-note mb-0">{{ t('app.modals.tunnel.regionProbeFailed') }}</p>
  </div>
</template>
