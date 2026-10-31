<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Dropdown } from 'bootstrap'
import IconActionButton from './IconActionButton.vue'

defineProps({
  primary: { type: Object, default: null },
  items: { type: Array, default: () => [] },
  menuLabel: { type: String, required: true },
  trigger: { type: String, default: 'icon' },
})

const emit = defineEmits(['primary', 'select'])

const toggleRef = ref(null)
const readyAttr = 'data-provider-dropdown'
let dropdown = null

function upsertModifier(modifiers, modifier) {
  const next = Array.isArray(modifiers) ? [...modifiers] : []
  const index = next.findIndex((item) => item?.name === modifier.name)
  if (index >= 0) next[index] = modifier
  else next.push(modifier)
  return next
}

// 菜单用 fixed，避免被表格横向滚动区域裁切。
function popperConfig(config) {
  const base = config || {}
  let modifiers = Array.isArray(base.modifiers) ? [...base.modifiers] : []
  modifiers = upsertModifier(modifiers, {
    name: 'flip',
    options: { fallbackPlacements: ['top-end', 'top-start', 'bottom-start'] },
  })
  modifiers = upsertModifier(modifiers, {
    name: 'preventOverflow',
    options: { boundary: 'viewport', padding: 8 },
  })
  modifiers = upsertModifier(modifiers, {
    name: 'computeStyles',
    options: { adaptive: false },
  })
  return { ...base, placement: 'bottom-end', strategy: 'fixed', modifiers }
}

function ensureDropdown() {
  const el = toggleRef.value
  if (!(el instanceof HTMLElement)) return null
  const existing = Dropdown.getInstance?.(el)
  if (existing && el.getAttribute(readyAttr) === '1') {
    dropdown = existing
    return existing
  }
  existing?.dispose?.()
  dropdown = new Dropdown(el, { popperConfig })
  el.setAttribute(readyAttr, '1')
  return dropdown
}

function closeOtherMenus(current) {
  document.querySelectorAll('.provider-row-menu .dropdown-toggle').forEach((toggle) => {
    if (toggle === current) return
    Dropdown.getInstance?.(toggle)?.hide?.()
  })
}

function toggleMenu(event) {
  event.preventDefault()
  event.stopPropagation()
  const current = event.currentTarget
  if (current instanceof HTMLElement) closeOtherMenus(current)
  ensureDropdown()?.toggle?.()
}

function onSelect(item) {
  if (!item || item.disabled) return
  dropdown?.hide?.()
  emit('select', item.key)
}

onMounted(() => {
  ensureDropdown()
})

onBeforeUnmount(() => {
  const el = toggleRef.value
  const instance = dropdown || (el && Dropdown.getInstance?.(el))
  instance?.dispose?.()
  dropdown = null
  el?.removeAttribute?.(readyAttr)
})
</script>

<template>
  <div class="provider-row-actions">
    <IconActionButton
      v-if="primary"
      button-class="provider-icon-btn"
      :title="primary.label"
      :aria-label="primary.label"
      :icon-class="primary.icon"
      :disabled="!!primary.disabled"
      @click="emit('primary')"
    />
    <div class="dropdown provider-row-menu">
      <button
        ref="toggleRef"
        type="button"
        class="provider-more-btn dropdown-toggle"
        :class="{ 'is-text': trigger === 'text' }"
        data-bs-toggle="dropdown"
        aria-haspopup="menu"
        aria-expanded="false"
        :aria-label="menuLabel"
        @click="toggleMenu"
      >
        <i v-if="trigger !== 'text'" class="bi bi-three-dots action-icon" aria-hidden="true" />
        <span v-else>{{ menuLabel }}</span>
      </button>
      <ul class="dropdown-menu dropdown-menu-end">
        <template v-for="item in items" :key="item.key">
          <li v-if="item.separatorBefore"><hr class="dropdown-divider" /></li>
          <li>
            <button
              type="button"
              class="dropdown-item"
              :class="{ 'text-danger': item.danger }"
              :disabled="!!item.disabled"
              @click="onSelect(item)"
            >
              {{ item.label }}
            </button>
          </li>
        </template>
      </ul>
    </div>
  </div>
</template>
