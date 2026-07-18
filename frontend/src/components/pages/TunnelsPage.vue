<script setup>
import IconActionButton from '../common/IconActionButton.vue'
import TooltipText from '../common/TooltipText.vue'
import { Dropdown } from 'bootstrap'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  tunnels: {
    type: Array,
    required: true
  },
  groups: {
    type: Array,
    default: () => []
  },
  hideEmptyUngrouped: {
    type: Boolean,
    default: true
  },
  searchQuery: {
    type: String,
    default: ''
  },
  modeOptions: {
    type: Array,
    required: true
  },
  tunnelAiDebugStates: {
    type: Object,
    required: true
  },
  aiDebugEnabled: {
    type: Boolean,
    default: false
  },
  getTunnelJumperLabel: {
    type: Function,
    required: true
  }
})

const emit = defineEmits([
  'toggle-tunnel',
  'copy-tunnel',
  'edit-tunnel',
  'delete-tunnel',
  'update-search-query',
  'ai-debug',
  'manage-groups',
  'rename-group',
  'delete-group',
  'move-tunnel-to-group'
])
const { t } = useI18n()

const UNGROUPED_SECTION_KEY = 'ungrouped'
const COLLAPSED_GROUPS_STORAGE_KEY = 'lt.tunnel-groups.collapsed'

const rootRef = ref(null)
const localSearchQuery = ref(props.searchQuery)
const expandedErrorIds = ref(new Set())
const copiedErrorIds = ref(new Set())
const copyResetTimers = new Map()
const collapsedSectionKeys = ref(new Set())
const ACTION_DROPDOWN_READY_ATTR = 'data-lt-action-dropdown-ready'

const showGroupedView = computed(() => Array.isArray(props.groups) && props.groups.length > 0)

const validGroupIds = computed(() => new Set(props.groups.map((group) => Number(group.id))))

const tunnelSections = computed(() => {
  if (!showGroupedView.value) return []

  const grouped = new Map()
  for (const group of props.groups) {
    grouped.set(Number(group.id), [])
  }

  const ungrouped = []
  for (const tunnel of props.tunnels) {
    const groupId = resolveTunnelGroupId(tunnel)
    if (groupId > 0) {
      const bucket = grouped.get(groupId)
      if (bucket) bucket.push(tunnel)
      else ungrouped.push(tunnel)
    } else {
      ungrouped.push(tunnel)
    }
  }

  const searching = Boolean(localSearchQuery.value.trim())
  const sections = []

  for (const group of props.groups) {
    const items = grouped.get(Number(group.id)) || []
    if (searching && items.length === 0) continue
    sections.push({
      key: String(group.id),
      groupId: Number(group.id),
      name: group.name,
      tunnels: items,
      isUngrouped: false
    })
  }

  if (ungrouped.length > 0 || (!searching && !props.hideEmptyUngrouped)) {
    sections.push({
      key: UNGROUPED_SECTION_KEY,
      groupId: 0,
      name: t('app.tunnels.groups.ungrouped'),
      tunnels: ungrouped,
      isUngrouped: true
    })
  }

  return sections
})

const visibleEntries = computed(() => {
  if (props.tunnels.length === 0) {
    return [{ kind: 'empty', key: 'empty' }]
  }

  if (!showGroupedView.value) {
    return props.tunnels.flatMap((tunnel) => [
      { kind: 'tunnel', key: `tunnel-${tunnel.id}`, tunnel },
      ...(isErrorExpanded(tunnel.id)
        ? [{ kind: 'error', key: `error-${tunnel.id}`, tunnel }]
        : [])
    ])
  }

  const entries = []
  for (const section of tunnelSections.value) {
    entries.push({ kind: 'group', key: `group-${section.key}`, section })
    if (isSectionCollapsed(section.key)) continue
    for (const tunnel of section.tunnels) {
      entries.push({ kind: 'tunnel', key: `tunnel-${tunnel.id}`, tunnel })
      if (isErrorExpanded(tunnel.id)) {
        entries.push({ kind: 'error', key: `error-${tunnel.id}`, tunnel })
      }
    }
  }
  return entries
})

watch(() => props.searchQuery, (newValue) => {
  localSearchQuery.value = newValue
})

watch(localSearchQuery, (newValue) => {
  emit('update-search-query', newValue)
  if (!newValue.trim()) return
  const next = new Set(collapsedSectionKeys.value)
  tunnelSections.value.forEach((section) => {
    if (section.tunnels.length > 0) {
      next.delete(section.key)
    }
  })
  collapsedSectionKeys.value = next
})

watch(
  () => props.tunnels,
  (nextTunnels) => {
    const validErrorIds = new Set(
      nextTunnels
        .filter((tunnel) => tunnel.status === 'error' && tunnel.lastError)
        .map((tunnel) => tunnel.id)
    )
    expandedErrorIds.value = new Set([...expandedErrorIds.value].filter((id) => validErrorIds.has(id)))
    copiedErrorIds.value = new Set([...copiedErrorIds.value].filter((id) => validErrorIds.has(id)))
    copyResetTimers.forEach((timerId, id) => {
      if (!validErrorIds.has(id)) {
        clearTimeout(timerId)
        copyResetTimers.delete(id)
      }
    })
  },
  { deep: true }
)

onBeforeUnmount(() => {
  copyResetTimers.forEach((timerId) => {
    clearTimeout(timerId)
  })
  copyResetTimers.clear()
})

onMounted(() => {
  loadCollapsedSections()
})

function loadCollapsedSections() {
  if (typeof window === 'undefined') return
  try {
    const raw = window.localStorage.getItem(COLLAPSED_GROUPS_STORAGE_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) {
      collapsedSectionKeys.value = new Set(parsed.map((item) => String(item)))
    }
  } catch (_) {
    collapsedSectionKeys.value = new Set()
  }
}

function persistCollapsedSections() {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(COLLAPSED_GROUPS_STORAGE_KEY, JSON.stringify([...collapsedSectionKeys.value]))
}

function resolveTunnelGroupId(tunnel) {
  const groupId = Number(tunnel?.groupId) || 0
  if (groupId > 0 && validGroupIds.value.has(groupId)) return groupId
  return 0
}

function isSectionCollapsed(sectionKey) {
  return collapsedSectionKeys.value.has(sectionKey)
}

function toggleSectionCollapsed(sectionKey) {
  const next = new Set(collapsedSectionKeys.value)
  if (next.has(sectionKey)) {
    next.delete(sectionKey)
  } else {
    next.add(sectionKey)
  }
  collapsedSectionKeys.value = next
  persistCollapsedSections()
}

function getMoveGroupOptions(tunnel) {
  const currentGroupId = resolveTunnelGroupId(tunnel)
  const options = props.groups
    .filter((group) => Number(group.id) !== currentGroupId)
    .map((group) => ({
      groupId: Number(group.id),
      label: group.name
    }))
  if (currentGroupId !== 0) {
    options.unshift({
      groupId: 0,
      label: t('app.tunnels.groups.ungrouped')
    })
  }
  return options
}

function onGroupActionSelect(event, action, section) {
  hideActionDropdown(event)
  if (action === 'rename') {
    emit('rename-group', { id: section.groupId, name: section.name })
    return
  }
  if (action === 'delete') {
    emit('delete-group', { id: section.groupId, name: section.name })
  }
}

function getModeLabel(modeValue) {
  return props.modeOptions.find((mode) => mode.value === modeValue)?.label || modeValue
}

function getStatusBadgeClass(status) {
  return {
    running: status === 'running',
    busy: status === 'busy',
    error: status === 'error',
    stopped: status !== 'running' && status !== 'busy' && status !== 'error'
  }
}

function getPrimaryActionButtonClass(status) {
  if (status === 'running') return 'btn-outline-danger'
  if (status === 'busy') return 'btn-outline-secondary'
  if (status === 'error') return 'btn-outline-warning'
  return 'btn-outline-success'
}

function getPrimaryActionTitle(status) {
  if (status === 'running') return 'app.tunnels.actions.stop'
  if (status === 'busy') return 'app.tunnels.actions.busy'
  if (status === 'error') return 'app.tunnels.actions.retry'
  return 'app.tunnels.actions.start'
}

function getPrimaryActionIcon(status) {
  if (status === 'running') return 'bi-pause-fill'
  if (status === 'busy') return 'bi-arrow-repeat spin'
  if (status === 'error') return 'bi-arrow-repeat'
  return 'bi-power'
}

function getMenuToggleButtonClass(status) {
  if (status === 'running') return 'btn-outline-danger'
  if (status === 'busy') return 'btn-outline-secondary'
  if (status === 'error') return 'btn-outline-warning'
  return 'btn-outline-success'
}

function getStatusLabelKey(status) {
  switch (status) {
    case 'running':
      return 'app.tunnels.status.running'
    case 'stopped':
      return 'app.tunnels.status.stopped'
    case 'busy':
      return 'app.tunnels.status.busy'
    case 'error':
      return 'app.tunnels.status.error'
    default:
      return ''
  }
}

function getRouteLines(tunnel) {
  const local = `${tunnel.localHost}:${tunnel.localPort}`
  const remote = `${tunnel.remoteHost}:${tunnel.remotePort}`
  if (tunnel.mode === 'dynamic') {
    return {
      top: local,
      bottom: 'SOCKS5'
    }
  }
  if (tunnel.mode === 'remote') {
    return {
      top: remote,
      bottom: local
    }
  }
  return {
    top: local,
    bottom: remote
  }
}

function getRouteTop(tunnel) {
  return getRouteLines(tunnel).top
}

function getRouteBottom(tunnel) {
  return getRouteLines(tunnel).bottom
}

// Keep latency readable in a compact table cell by switching units automatically.
function formatLatencyLabel(latencyMs) {
  const ms = Number(latencyMs)
  if (!Number.isFinite(ms) || ms <= 0) return '--'
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(ms < 10000 ? 2 : 1)} s`
  if (ms < 3600000) return `${(ms / 60000).toFixed(ms < 600000 ? 2 : 1)} min`
  return `${(ms / 3600000).toFixed(2)} h`
}

// Show placeholder/measuring state when tunnel is not running or latency is not ready yet.
function getTunnelLatencyLabel(tunnel) {
  if (tunnel.status !== 'running') return '--'
  if (!tunnel.latencyMs) return t('app.tunnels.latency.measuring')
  return formatLatencyLabel(tunnel.latencyMs)
}

function canToggleErrorDetails(tunnel) {
  return tunnel.status === 'error' && Boolean(tunnel.lastError)
}

function isErrorExpanded(tunnelId) {
  return expandedErrorIds.value.has(tunnelId)
}

function toggleErrorDetails(tunnel) {
  if (!canToggleErrorDetails(tunnel)) return
  const next = new Set(expandedErrorIds.value)
  if (next.has(tunnel.id)) {
    next.delete(tunnel.id)
  } else {
    next.add(tunnel.id)
  }
  expandedErrorIds.value = next
}

function isErrorCopied(tunnelId) {
  return copiedErrorIds.value.has(tunnelId)
}

async function writeTextToClipboard(text) {
  if (navigator?.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch (err) {
      // fallback below
    }
  }

  try {
    const fallbackInput = document.createElement('textarea')
    fallbackInput.value = text
    fallbackInput.setAttribute('readonly', '')
    fallbackInput.style.position = 'fixed'
    fallbackInput.style.top = '-9999px'
    fallbackInput.style.left = '-9999px'
    fallbackInput.style.opacity = '0'
    document.body.appendChild(fallbackInput)
    fallbackInput.focus()
    fallbackInput.select()
    const copied = document.execCommand('copy')
    document.body.removeChild(fallbackInput)
    return copied
  } catch (err) {
    return false
  }
}

async function copyErrorDetails(tunnel) {
  if (!tunnel.lastError) return
  try {
    const copied = await writeTextToClipboard(tunnel.lastError)
    if (!copied) return
    const next = new Set(copiedErrorIds.value)
    next.add(tunnel.id)
    copiedErrorIds.value = next
    const oldTimer = copyResetTimers.get(tunnel.id)
    if (oldTimer) {
      clearTimeout(oldTimer)
    }
    const timerId = setTimeout(() => {
      const nextCopied = new Set(copiedErrorIds.value)
      nextCopied.delete(tunnel.id)
      copiedErrorIds.value = nextCopied
      copyResetTimers.delete(tunnel.id)
    }, 1800)
    copyResetTimers.set(tunnel.id, timerId)
  } catch (err) {
    console.warn('Failed to copy tunnel error details:', err)
  }
}

function getErrorToggleLabelKey(tunnel) {
  if (!canToggleErrorDetails(tunnel)) return ''
  return isErrorExpanded(tunnel.id) ? 'app.tunnels.actions.collapseError' : 'app.tunnels.actions.expandError'
}

function getErrorCopyLabelKey(tunnelId) {
  return isErrorCopied(tunnelId) ? 'app.tunnels.actions.copied' : 'app.tunnels.actions.copyError'
}

function getTunnelAiDebugState(tunnelId) {
  return props.tunnelAiDebugStates?.[tunnelId] || { status: 'idle', error: '', result: null }
}

function getAIDebugActionLabel(tunnelId) {
  const state = getTunnelAiDebugState(tunnelId)
  if (state.status === 'analyzing') return t('app.aiDebug.analyzing')
  if (state.status === 'success' || state.status === 'error') return t('app.aiDebug.viewResult')
  return t('app.aiDebug.action')
}

// Merge/replace a Popper modifier by name to avoid duplicate modifier entries.
function upsertPopperModifier(modifiers, modifier) {
  const next = Array.isArray(modifiers) ? [...modifiers] : []
  const index = next.findIndex((item) => item?.name === modifier.name)
  if (index >= 0) {
    next[index] = modifier
  } else {
    next.push(modifier)
  }
  return next
}

// Use fixed positioning + viewport overflow guard so row menus stay visible
// inside scrollable table containers.
function getActionDropdownInstance(toggleEl) {
  const existing = Dropdown.getInstance(toggleEl)
  const isReady = toggleEl.getAttribute(ACTION_DROPDOWN_READY_ATTR) === '1'
  if (existing && isReady) {
    return existing
  }
  if (existing) {
    existing.dispose()
  }

  const instance = new Dropdown(toggleEl, {
    popperConfig(defaultBsPopperConfig) {
      const base = defaultBsPopperConfig || {}
      let modifiers = Array.isArray(base.modifiers) ? [...base.modifiers] : []
      modifiers = upsertPopperModifier(modifiers, {
        name: 'flip',
        enabled: true,
        options: {
          fallbackPlacements: ['top-end', 'top-start', 'bottom-start']
        }
      })
      modifiers = upsertPopperModifier(modifiers, {
        name: 'computeStyles',
        options: {
          adaptive: false
        }
      })
      modifiers = upsertPopperModifier(modifiers, {
        name: 'preventOverflow',
        options: {
          boundary: 'viewport',
          padding: 8
        }
      })
      return {
        ...base,
        placement: 'bottom-end',
        strategy: 'fixed',
        modifiers
      }
    }
  })
  toggleEl.setAttribute(ACTION_DROPDOWN_READY_ATTR, '1')
  return instance
}

// Keep only one row action menu open at a time for cleaner interaction.
function toggleActionDropdown(event) {
  const current = event?.currentTarget
  if (!(current instanceof HTMLElement)) return

  const root = rootRef.value
  if (root) {
    const openedMenus = root.querySelectorAll('.tunnel-action-menu .dropdown-menu.show')
    openedMenus.forEach((menu) => {
      const parent = menu.closest('.tunnel-action-menu')
      if (!parent || parent.contains(current)) return
      const toggle = parent.querySelector('.dropdown-toggle')
      if (toggle instanceof HTMLElement) {
        getActionDropdownInstance(toggle).hide()
      }
    })
  }

  getActionDropdownInstance(current).toggle()
}

function hideActionDropdown(event) {
  const current = event?.currentTarget
  if (!(current instanceof HTMLElement)) return
  const menu = current.closest('.tunnel-action-menu')
  if (!menu) return
  const toggle = menu.querySelector('.dropdown-toggle')
  if (!(toggle instanceof HTMLElement)) return
  getActionDropdownInstance(toggle).hide()
}

// Close dropdown first, then execute the selected action callback.
function onActionSelect(event, action, tunnel, payload = null) {
  hideActionDropdown(event)
  if (action === 'edit') {
    emit('edit-tunnel', tunnel)
    return
  }
  if (action === 'copy') {
    emit('copy-tunnel', tunnel)
    return
  }
  if (action === 'move') {
    emit('move-tunnel-to-group', { tunnel, groupId: payload })
    return
  }
  emit('delete-tunnel', tunnel)
}
</script>

<template>
  <section ref="rootRef" class="page-fade panel-card">
    <div class="panel-head">
      <h2 class="panel-title mb-0">{{ $t('app.tunnels.title') }}</h2>
      <div class="panel-head-actions">
        <button type="button" class="btn btn-sm btn-outline-secondary" @click="emit('manage-groups')">
          <i class="bi bi-folder2 me-1" />
          {{ $t('app.tunnels.groups.manage') }}
        </button>
        <div class="search-box">
        <div class="input-group input-group-sm">
          <span class="input-group-text">
            <i class="bi bi-search"></i>
          </span>
          <input
            v-model="localSearchQuery"
            type="text"
            class="form-control"
            :placeholder="$t('app.common.searchPlaceholder')"
            aria-label="Search tunnels"
          />
          <button
            class="btn btn-outline-secondary"
            :class="{ invisible: !localSearchQuery }"
            type="button"
            @click="localSearchQuery = ''"
            :aria-label="$t('app.common.clearSearch')"
            :disabled="!localSearchQuery"
          >
            <i class="bi bi-x"></i>
          </button>
        </div>
        </div>
      </div>
    </div>
    <div class="table-responsive page-table-wrap tunnels-table-wrap">
      <table class="table align-middle mb-0 tunnels-table">
        <thead>
          <tr>
            <th class="tunnel-name-cell">{{ $t('app.tunnels.table.name') }}</th>
            <th class="tunnel-mode-cell">{{ $t('app.tunnels.table.mode') }}</th>
            <th class="tunnel-route-cell">{{ $t('app.tunnels.table.route') }}</th>
            <th class="tunnel-jumper-cell">{{ $t('app.tunnels.table.jumper') }}</th>
            <th class="tunnel-status-cell">{{ $t('app.tunnels.table.status') }}</th>
            <th class="tunnel-latency-cell">{{ $t('app.tunnels.table.latency') }}</th>
            <th class="text-end tunnels-action-cell">{{ $t('app.tunnels.table.action') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="visibleEntries[0]?.kind === 'empty'">
            <td colspan="7" class="text-muted py-4">{{ $t('app.tunnels.noTunnels') }}</td>
          </tr>
          <template v-for="entry in visibleEntries" :key="entry.key">
            <tr
              v-if="entry.kind === 'group'"
              class="tunnel-group-row"
              :class="{ 'is-collapsed': isSectionCollapsed(entry.section.key) }"
              @click="toggleSectionCollapsed(entry.section.key)"
            >
              <td colspan="7">
                <div class="tunnel-group-row-inner">
                  <button
                    type="button"
                    class="btn btn-sm btn-link tunnel-group-toggle p-0"
                    :aria-expanded="!isSectionCollapsed(entry.section.key)"
                    :aria-label="entry.section.name"
                    @click.stop="toggleSectionCollapsed(entry.section.key)"
                  >
                    <i class="bi" :class="isSectionCollapsed(entry.section.key) ? 'bi-chevron-right' : 'bi-chevron-down'" />
                  </button>
                  <span class="tunnel-group-name">{{ entry.section.name }}</span>
                  <span class="tunnel-group-count text-muted">({{ entry.section.tunnels.length }})</span>
                    <div v-if="!entry.section.isUngrouped" class="tunnel-group-actions ms-auto" @click.stop>
                    <div class="btn-group btn-group-sm tunnel-action-menu tunnel-group-menu" role="group">
                      <button
                        type="button"
                        class="btn icon-btn action-menu-toggle dropdown-toggle"
                        data-bs-toggle="dropdown"
                        aria-expanded="false"
                        :title="$t('app.tunnels.actions.more')"
                        :aria-label="$t('app.tunnels.actions.more')"
                        @click.stop.prevent="toggleActionDropdown($event)"
                      >
                        <i class="bi bi-three-dots action-icon" />
                      </button>
                      <ul class="dropdown-menu dropdown-menu-end">
                        <li>
                          <button
                            type="button"
                            class="dropdown-item d-flex align-items-center gap-2"
                            @click="onGroupActionSelect($event, 'rename', entry.section)"
                          >
                            <i class="bi bi-pencil opacity-75" />
                            <span>{{ $t('app.tunnels.groups.rename') }}</span>
                          </button>
                        </li>
                        <li><hr class="dropdown-divider" /></li>
                        <li>
                          <button
                            type="button"
                            class="dropdown-item d-flex align-items-center gap-2 text-danger"
                            @click="onGroupActionSelect($event, 'delete', entry.section)"
                          >
                            <i class="bi bi-trash3" />
                            <span>{{ $t('app.tunnels.groups.delete') }}</span>
                          </button>
                        </li>
                      </ul>
                    </div>
                  </div>
                </div>
              </td>
            </tr>
            <tr v-else-if="entry.kind === 'tunnel'">
              <td class="fw-semibold tunnel-name-cell">
                <TooltipText :text="entry.tunnel.name" class-name="cell-ellipsis" />
              </td>
              <td class="tunnel-mode-cell">{{ getModeLabel(entry.tunnel.mode) }}</td>
              <td class="text-muted tunnel-route-cell">
                <div class="tunnel-route-wrap">
                  <TooltipText :text="getRouteTop(entry.tunnel)" class-name="cell-ellipsis route-line" />
                  <TooltipText :text="getRouteBottom(entry.tunnel)" class-name="cell-ellipsis route-line route-line-secondary" />
                </div>
              </td>
              <td class="tunnel-jumper-cell">
                <TooltipText :text="getTunnelJumperLabel(entry.tunnel)" class-name="cell-ellipsis" />
              </td>
              <td class="tunnel-status-cell">
                <div class="tunnel-status-wrap">
                  <span
                    class="status-badge"
                    :class="[getStatusBadgeClass(entry.tunnel.status), { 'status-badge-expandable': canToggleErrorDetails(entry.tunnel) }]"
                    :role="canToggleErrorDetails(entry.tunnel) ? 'button' : undefined"
                    :tabindex="canToggleErrorDetails(entry.tunnel) ? 0 : undefined"
                    :aria-label="canToggleErrorDetails(entry.tunnel) ? $t(getErrorToggleLabelKey(entry.tunnel)) : undefined"
                    :aria-expanded="canToggleErrorDetails(entry.tunnel) ? isErrorExpanded(entry.tunnel.id) : undefined"
                    @click="toggleErrorDetails(entry.tunnel)"
                    @keydown.enter.prevent="toggleErrorDetails(entry.tunnel)"
                    @keydown.space.prevent="toggleErrorDetails(entry.tunnel)"
                  >
                    <span>{{ getStatusLabelKey(entry.tunnel.status) ? $t(getStatusLabelKey(entry.tunnel.status)) : entry.tunnel.status }}</span>
                    <i
                      v-if="canToggleErrorDetails(entry.tunnel)"
                      class="bi status-badge-toggle-icon"
                      :class="isErrorExpanded(entry.tunnel.id) ? 'bi-chevron-up' : 'bi-chevron-down'"
                    />
                  </span>
                </div>
              </td>
              <td class="tunnel-latency-cell text-muted">
                <span class="cell-ellipsis" :title="getTunnelLatencyLabel(entry.tunnel)">{{ getTunnelLatencyLabel(entry.tunnel) }}</span>
              </td>
              <td class="text-end tunnels-action-cell">
                <div class="btn-group btn-group-sm action-btn-group tunnel-action-menu" role="group" aria-label="Tunnel Actions">
                  <IconActionButton
                    :button-class="getPrimaryActionButtonClass(entry.tunnel.status)"
                    :title="$t(getPrimaryActionTitle(entry.tunnel.status))"
                    :aria-label="$t(getPrimaryActionTitle(entry.tunnel.status))"
                    :icon-class="getPrimaryActionIcon(entry.tunnel.status)"
                    :disabled="entry.tunnel.status === 'busy'"
                    @click="$emit('toggle-tunnel', entry.tunnel)"
                  />
                  <button
                    type="button"
                    class="btn icon-btn action-menu-toggle dropdown-toggle dropdown-toggle-split"
                    :class="getMenuToggleButtonClass(entry.tunnel.status)"
                    data-bs-toggle="dropdown"
                    aria-expanded="false"
                    :title="$t('app.tunnels.actions.more')"
                    :aria-label="$t('app.tunnels.actions.more')"
                    @click.stop.prevent="toggleActionDropdown($event)"
                  >
                    <i class="bi bi-three-dots action-icon" />
                    <span class="visually-hidden">{{ $t('app.tunnels.actions.more') }}</span>
                  </button>
                  <ul class="dropdown-menu dropdown-menu-end">
                    <li>
                      <button
                        type="button"
                        class="dropdown-item d-flex align-items-center gap-2"
                        :disabled="entry.tunnel.status === 'busy'"
                        @click="onActionSelect($event, 'edit', entry.tunnel)"
                      >
                        <i class="bi bi-sliders opacity-75" />
                        <span>{{ $t('app.tunnels.actions.edit') }}</span>
                      </button>
                    </li>
                    <li>
                      <button
                        type="button"
                        class="dropdown-item d-flex align-items-center gap-2"
                        @click="onActionSelect($event, 'copy', entry.tunnel)"
                      >
                        <i class="bi bi-copy opacity-75" />
                        <span>{{ $t('app.tunnels.actions.copy') }}</span>
                      </button>
                    </li>
                    <li v-if="showGroupedView && getMoveGroupOptions(entry.tunnel).length > 0">
                      <hr class="dropdown-divider" />
                    </li>
                    <template v-if="showGroupedView && getMoveGroupOptions(entry.tunnel).length > 0">
                      <li class="tunnel-move-group-block">
                        <div class="tunnel-move-group-title">
                          <i class="bi bi-folder2 opacity-75" aria-hidden="true" />
                          <span>{{ $t('app.tunnels.groups.moveTo') }}</span>
                        </div>
                        <button
                          v-for="option in getMoveGroupOptions(entry.tunnel)"
                          :key="`move-${entry.tunnel.id}-${option.groupId}`"
                          type="button"
                          class="dropdown-item tunnel-move-group-option"
                          :disabled="entry.tunnel.status === 'busy'"
                          :title="option.label"
                          @click="onActionSelect($event, 'move', entry.tunnel, option.groupId)"
                        >
                          <span class="tunnel-move-group-option-label">{{ option.label }}</span>
                        </button>
                      </li>
                    </template>
                    <li><hr class="dropdown-divider" /></li>
                    <li>
                      <button
                        type="button"
                        class="dropdown-item d-flex align-items-center gap-2 text-danger"
                        :disabled="entry.tunnel.status === 'busy'"
                        @click="onActionSelect($event, 'delete', entry.tunnel)"
                      >
                        <i class="bi bi-trash3" />
                        <span>{{ $t('app.tunnels.actions.delete') }}</span>
                      </button>
                    </li>
                  </ul>
                </div>
              </td>
            </tr>
            <tr v-else-if="entry.kind === 'error'" class="tunnel-error-detail-row">
              <td colspan="7">
                <div class="tunnel-error-detail">
                  <div class="tunnel-error-detail-content">
                    <div class="tunnel-error-detail-label">{{ $t('app.tunnels.errorReason') }}</div>
                    <div class="tunnel-error-detail-message">{{ entry.tunnel.lastError }}</div>
                    <div v-if="aiDebugEnabled" class="mt-2">
                      <button
                        type="button"
                        class="btn btn-sm btn-outline-primary ai-debug-action-btn"
                        :disabled="getTunnelAiDebugState(entry.tunnel.id).status === 'analyzing'"
                        @click="$emit('ai-debug', entry.tunnel)"
                      >
                        <i class="bi" :class="getTunnelAiDebugState(entry.tunnel.id).status === 'analyzing' ? 'bi-hourglass-split' : 'bi-magic'" />
                        <span>{{ getAIDebugActionLabel(entry.tunnel.id) }}</span>
                      </button>
                      <div v-if="getTunnelAiDebugState(entry.tunnel.id).result?.reason" class="tunnel-error-ai-summary mt-2">
                        {{ getTunnelAiDebugState(entry.tunnel.id).result.reason }}
                      </div>
                    </div>
                  </div>
                  <button
                    type="button"
                    class="btn btn-sm tunnel-error-copy-btn"
                    :class="{ 'is-copied': isErrorCopied(entry.tunnel.id) }"
                    :aria-label="$t(getErrorCopyLabelKey(entry.tunnel.id))"
                    :title="$t(getErrorCopyLabelKey(entry.tunnel.id))"
                    @click="copyErrorDetails(entry.tunnel)"
                  >
                    <i class="bi" :class="isErrorCopied(entry.tunnel.id) ? 'bi-check2' : 'bi-copy'" />
                  </button>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </section>
</template>
