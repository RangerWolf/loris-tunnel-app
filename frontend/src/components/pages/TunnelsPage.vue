<script setup>
import { devTunnelReconnectText } from '../../utils/devtunnel-reconnect'
import { frpState, frpAddress } from '../../utils/frp'
import { latencyLabel, latencyTitle, formatLatencyLabel } from '../../utils/tunnel-latency'
import IconActionButton from '../common/IconActionButton.vue'
import CopyIconButton from '../common/CopyIconButton.vue'
import SoftActionButton from '../common/SoftActionButton.vue'
import TooltipText from '../common/TooltipText.vue'
import { Dropdown } from 'bootstrap'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  pendingTunnelActions: {
    type: Map,
    required: true
  },
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
  },
  describeDevTunnel: {
    type: Function,
    default: null
  },
  describeCloudflare: {
    type: Function,
    default: null
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
  'move-tunnel-to-group',
  'open-url',
  'open-provider'
])
const { t } = useI18n()

const UNGROUPED_SECTION_KEY = 'ungrouped'
const COLLAPSED_GROUPS_STORAGE_KEY = 'lt.tunnel-groups.collapsed'

const rootRef = ref(null)
const localSearchQuery = ref(props.searchQuery)
const statusFilter = ref('all')
const providerFilter = ref('all')
const providerOptions = computed(() => [
  { value: 'all', label: t('app.tunnels.allProviders') },
  { value: 'ssh', label: t('app.providers.ssh') },
  { value: 'devtunnel', label: t('app.providers.devtunnel') },
  { value: 'frp', label: 'FRP' },
  { value: 'cloudflare', label: t('app.providers.cloudflare') },
])
const groupViewMode = ref('groups')
const statusOptions = computed(() => [
  { key: 'all', label: t('app.overview.all'), count: props.tunnels.length },
  { key: 'running', label: t('app.tunnels.status.running'), count: props.tunnels.filter((tunnel) => tunnel.status === 'running').length },
  { key: 'error', label: t('app.tunnels.status.error'), count: props.tunnels.filter((tunnel) => isListedError(tunnel.status)).length },
  { key: 'stopped', label: t('app.tunnels.status.stopped'), count: props.tunnels.filter((tunnel) => tunnel.status === 'stopped').length }
])
const matchingTunnels = computed(() => props.tunnels.filter((tunnel) => {
  const statusOk = statusFilter.value === 'all' || (statusFilter.value === 'error' ? isListedError(tunnel.status) : tunnel.status === statusFilter.value)
  const provider = tunnel.provider || 'ssh'
  const providerOk = providerFilter.value === 'all' || provider === providerFilter.value
  return statusOk && providerOk
}))
const expandedErrorIds = ref(new Set())
const collapsedSectionKeys = ref(new Set())
const ACTION_DROPDOWN_READY_ATTR = 'data-lt-action-dropdown-ready'

const showGroupedView = computed(() => groupViewMode.value === 'groups' && Array.isArray(props.groups) && props.groups.length > 0)

const validGroupIds = computed(() => new Set(props.groups.map((group) => Number(group.id))))

const tunnelSections = computed(() => {
  if (!showGroupedView.value) return []

  const grouped = new Map()
  for (const group of props.groups) {
    grouped.set(Number(group.id), [])
  }

  const ungrouped = []
  for (const tunnel of matchingTunnels.value) {
    const groupId = resolveTunnelGroupId(tunnel)
    if (groupId > 0) {
      const bucket = grouped.get(groupId)
      if (bucket) bucket.push(tunnel)
      else ungrouped.push(tunnel)
    } else {
      ungrouped.push(tunnel)
    }
  }

  const searching = Boolean(localSearchQuery.value.trim()) || statusFilter.value !== 'all' || providerFilter.value !== 'all'
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
  if (matchingTunnels.value.length === 0) {
    return [{ kind: 'empty', key: 'empty' }]
  }

  if (!showGroupedView.value) {
    return matchingTunnels.value.flatMap((tunnel) => [
      { kind: 'tunnel', key: `tunnel-${tunnel.id}`, tunnel },
      ...(isErrorExpanded(tunnel.id) && canToggleErrorDetails(tunnel)
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
      if (isErrorExpanded(tunnel.id) && canToggleErrorDetails(tunnel)) {
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
        .filter((tunnel) => canToggleErrorDetails(tunnel))
        .map((tunnel) => tunnel.id)
    )
    expandedErrorIds.value = new Set([...expandedErrorIds.value].filter((id) => validErrorIds.has(id)))

  },
  { deep: true }
)


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
  const key = `app.options.mode.${modeValue}`
  const translated = t(key)
  if (translated && translated !== key) return translated
  return props.modeOptions.find((mode) => mode.value === modeValue)?.label || modeValue
}

function tunnelProviderName(tunnel) {
  return tunnel?.provider || 'ssh'
}

function providerLabel(tunnel) {
  const provider = tunnelProviderName(tunnel)
  const key = `app.providers.${provider}`
  const translated = t(key)
  return translated && translated !== key ? translated : provider
}

function tunnelTypeLabel(tunnel) {
  const mode = getModeLabel(tunnel?.mode).replace(/\s*\([^)]*\)\s*$/, '')
  return `${providerLabel(tunnel)} - ${mode}`
}

function displayedStatus(tunnel) {
  if (props.pendingTunnelActions.has(tunnel.id)) return 'busy'
  return tunnel?.status
}

function isListedError(status) {
  return status === 'reconnecting' || status === 'error' || status === 'auth_required' || status === 'needs_setup'
}

function badgeStatus(tunnel) {
  const status = displayedStatus(tunnel)
  return status === 'reconnecting' ? status : isListedError(status) ? 'error' : status
}

function providerSetupAction(tunnel) {
  if (tunnel?.status === 'auth_required') return 'app.tunnels.status.auth_required'
  if (tunnel?.status === 'needs_setup') return 'app.tunnels.status.needs_setup'
  return ''
}

function primaryActionTitleKey(tunnel) {
  if (props.pendingTunnelActions.has(tunnel.id)) return 'app.tunnels.actions.busy'
  return getPrimaryActionTitle(displayedStatus(tunnel))
}

function getStatusBadgeClass(status) {
  return {
    running: status === 'running',
    busy: status === 'busy' || status === 'needs_setup' || status === 'reconnecting',
    error: status === 'error' || status === 'auth_required',
    stopped: status !== 'reconnecting' && status !== 'running' && status !== 'busy' && status !== 'error' && status !== 'needs_setup' && status !== 'auth_required'
  }
}

function getPrimaryActionButtonClass(status) {
  if (status === 'running' || status === 'reconnecting') return 'btn-outline-danger'
  if (status === 'busy') return 'btn-outline-secondary'
  if (status === 'error') return 'btn-outline-warning'
  return 'btn-outline-success'
}

function getPrimaryActionTitle(status) {
  if (status === 'running' || status === 'reconnecting') return 'app.tunnels.actions.stop'
  if (status === 'busy') return 'app.tunnels.actions.busy'
  if (status === 'error') return 'app.tunnels.actions.retry'
  return 'app.tunnels.actions.start'
}

function getPrimaryActionIcon(status) {
  if (status === 'running' || status === 'reconnecting') return 'bi-pause-fill'
  if (status === 'busy') return 'bi-arrow-repeat spin'
  if (status === 'error') return 'bi-arrow-repeat'
  return 'bi-power'
}

function getMenuToggleButtonClass(status) {
  if (status === 'running' || status === 'reconnecting') return 'btn-outline-danger'
  if (status === 'busy') return 'btn-outline-secondary'
  if (status === 'error') return 'btn-outline-warning'
  return 'btn-outline-success'
}

function getStatusLabelKey(status) {
  if (status === 'reconnecting') return 'app.tunnels.reconnect.label'
  switch (status) {
    case 'running':
      return 'app.tunnels.status.running'
    case 'stopped':
      return 'app.tunnels.status.stopped'
    case 'busy':
      return 'app.tunnels.status.busy'
    case 'error':
      return 'app.tunnels.status.error'
    case 'needs_setup':
      return 'app.tunnels.status.needs_setup'
    case 'auth_required':
      return 'app.tunnels.status.auth_required'
    default:
      return ''
  }
}

function devTunnelInfo(tunnel) {
  if (typeof props.describeDevTunnel === 'function') return props.describeDevTunnel(tunnel)
  const id = String(tunnel?.devTunnel?.tunnelId || '').trim()
  const url = String(tunnel?.publicUrl || '').split(',')[0].trim()
  return {
    connection: id || t('app.tunnels.connection.pendingCreate'),
    routeBottom: url || t(['running', 'busy'].includes(tunnel?.status) ? 'app.tunnels.waitingForUrl' : 'app.tunnels.publicUrlAfterStart'),
    routeTitle: url,
    url,
  }
}

function cloudflareInfo(tunnel) {
  if (typeof props.describeCloudflare === 'function') return props.describeCloudflare(tunnel)
  const id = String(tunnel?.cloudflare?.tunnelId || '').trim()
  const url = String(tunnel?.cloudflare?.hostname || '').trim() ? `https://${String(tunnel.cloudflare.hostname).trim()}` : ''
  return {
    connection: id || t('app.tunnels.connection.pendingCreate'),
    connectionTitle: id,
    routeBottom: url,
    routeTitle: url,
    url,
  }
}

function getRouteLines(tunnel) {
  const local = `${tunnel.localHost}:${tunnel.localPort}`
  if (tunnelProviderName(tunnel) === 'frp') {
    const server = frpState.servers.find(s => s.id === tunnel.frp?.serverId)
    const address = tunnel.publicUrl || frpAddress(server, tunnel.mode, tunnel.frp?.remotePort, tunnel.frp?.domain)
    return {
      top: local,
      bottom: address || t('app.frp.missingServer'),
      title: address,
      url: tunnel.status === 'running' && tunnel.mode === 'http' ? address : '',
    }
  }
  if (tunnelProviderName(tunnel) === 'cloudflare') {
    const info = cloudflareInfo(tunnel)
    return {
      top: local,
      bottom: info.routeBottom || t('app.cloudflare.publicUrlPending'),
      title: info.routeTitle || info.routeBottom,
      url: info.url,
    }
  }
  if (tunnelProviderName(tunnel) === 'devtunnel') {
    const info = devTunnelInfo(tunnel)
    const waiting = tunnel.status === 'running' || tunnel.status === 'busy'
    return {
      top: local,
      bottom: info.routeBottom || (waiting ? t('app.tunnels.waitingForUrl') : t('app.tunnels.publicUrlAfterStart')),
      title: info.routeTitle || info.routeBottom,
      url: info.url,
    }
  }
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


// Show placeholder/measuring state when tunnel is not running or latency is not ready yet.
function getTunnelLatencyLabel(tunnel) {
  return latencyLabel(tunnel, t, formatLatencyLabel)
}

function getTunnelLatencyTitle(tunnel) {
  return latencyTitle(tunnel, t)
}

function errorDetailText(tunnel) {
  if (tunnel?.provider === 'devtunnel' && tunnel.status === 'reconnecting' && tunnel.reconnect) return devTunnelReconnectText(tunnel.reconnect, t)
  const text = String(tunnel?.lastError || '').trim()
  if (text) return text
  if (tunnel?.status === 'auth_required') return t('app.tunnels.status.auth_required')
  if (tunnel?.status === 'needs_setup') return t('app.tunnels.status.needs_setup')
  return ''
}

function canToggleErrorDetails(tunnel) {
  return isListedError(tunnel?.status) && Boolean(errorDetailText(tunnel))
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



function getErrorToggleLabelKey(tunnel) {
  if (!canToggleErrorDetails(tunnel)) return ''
  return isErrorExpanded(tunnel.id) ? 'app.tunnels.actions.collapseError' : 'app.tunnels.actions.expandError'
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
  if (action === 'provider') {
    emit('open-provider', tunnel?.provider || 'devtunnel')
    return
  }
  emit('delete-tunnel', tunnel)
}
</script>

<template>
  <section ref="rootRef" class="page-fade panel-card">
    <div class="panel-head lt-tunnels-heading">
      <h2 class="panel-title mb-0">{{ $t('app.tunnels.title') }}</h2>
      <div class="panel-head-actions">
        <SoftActionButton @click="emit('manage-groups')">
          <i class="bi bi-folder2 me-1" />
          {{ $t('app.tunnels.groups.manage') }}
        </SoftActionButton>
        <div class="search-box">
        <div class="input-group input-group-sm">
          <span class="input-group-text">
            <i class="bi bi-search"></i>
          </span>
          <input
            v-model="localSearchQuery"
            type="text"
            class="form-control"
            autocapitalize="none"
            autocorrect="off"
            spellcheck="false"
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
    <div class="lt-tunnels-toolbar">
      <div class="lt-filter-chips" role="group" :aria-label="$t('app.overview.filterStatus')">
        <button
          v-for="option in statusOptions"
          :key="option.key"
          type="button"
          class="lt-filter-chip"
          :class="{ active: statusFilter === option.key }"
          :aria-pressed="statusFilter === option.key"
          @click="statusFilter = option.key"
        >{{ option.label }} <span class="lt-filter-count">{{ option.count }}</span></button>
      </div>
      <div class="lt-tunnel-filters">
        <label class="lt-group-view">
          <span>{{ $t('app.tunnels.providerFilter') }}</span>
          <select v-model="providerFilter" class="form-select form-select-sm" :aria-label="$t('app.tunnels.providerFilter')">
            <option v-for="option in providerOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>
        <label class="lt-group-view">
          <span>{{ $t('app.overview.groupBy') }}</span>
          <select v-model="groupViewMode" class="form-select form-select-sm" :aria-label="$t('app.overview.groupBy')">
            <option value="groups">{{ $t('app.overview.myGroups') }}</option>
            <option value="none">{{ $t('app.overview.none') }}</option>
          </select>
        </label>
      </div>
    </div>
    <div class="table-responsive page-table-wrap tunnels-table-wrap">
      <table class="table align-middle mb-0 tunnels-table">
        <thead>
          <tr>
            <th class="tunnel-name-cell">{{ $t('app.tunnels.table.name') }}</th>
            <th class="tunnel-route-cell">{{ $t('app.tunnels.table.route') }}</th>
            <th class="tunnel-jumper-cell">{{ $t('app.tunnels.table.jumper') }}</th>
            <th class="tunnel-status-cell">{{ $t('app.tunnels.table.status') }}</th>
            <th class="tunnel-latency-cell">{{ $t('app.tunnels.table.latency') }}</th>
            <th class="text-end tunnels-action-cell">{{ $t('app.tunnels.table.action') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="visibleEntries[0]?.kind === 'empty'">
            <td colspan="6" class="text-muted py-4">{{ $t('app.tunnels.noTunnels') }}</td>
          </tr>
          <template v-for="entry in visibleEntries" :key="entry.key">
            <tr
              v-if="entry.kind === 'group'"
              class="tunnel-group-row"
              :class="{ 'is-collapsed': isSectionCollapsed(entry.section.key) }"
              @click="toggleSectionCollapsed(entry.section.key)"
            >
              <td colspan="6">
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
                <span class="lt-tunnel-name">
                  <TooltipText :text="entry.tunnel.name" class-name="cell-ellipsis" />
                </span>
                <span class="lt-tunnel-secondary lt-tunnel-type-sub">{{ tunnelTypeLabel(entry.tunnel) }}</span>
              </td>
              <td class="text-muted tunnel-route-cell">
                <div class="tunnel-route-wrap">
                  <TooltipText :text="getRouteTop(entry.tunnel)" class-name="cell-ellipsis route-line" />
                  <span class="route-line route-line-secondary d-flex align-items-center min-w-0">
                    <TooltipText :text="getRouteBottom(entry.tunnel)" :title="getRouteLines(entry.tunnel).title" class-name="cell-ellipsis" />
                  </span>
                </div>
              </td>
              <td class="tunnel-jumper-cell">
                <TooltipText v-if="tunnelProviderName(entry.tunnel) === 'devtunnel'" :text="devTunnelInfo(entry.tunnel).connection" :title="devTunnelInfo(entry.tunnel).connectionTitle || devTunnelInfo(entry.tunnel).connection" class-name="cell-ellipsis" />
                <TooltipText v-else-if="tunnelProviderName(entry.tunnel) === 'cloudflare'" :text="cloudflareInfo(entry.tunnel).connection" :title="cloudflareInfo(entry.tunnel).connectionTitle || cloudflareInfo(entry.tunnel).connection" class-name="cell-ellipsis" />
                <TooltipText v-else-if="tunnelProviderName(entry.tunnel) === 'frp'" :text="frpState.servers.find(s => s.id === entry.tunnel.frp?.serverId)?.name || $t('app.frp.missingServer')" class-name="cell-ellipsis" />
                <TooltipText v-else :text="getTunnelJumperLabel(entry.tunnel)" class-name="cell-ellipsis" />
              </td>
              <td class="tunnel-status-cell">
                <div class="tunnel-status-wrap">
                  <span
                    class="status-badge"
                    :class="[getStatusBadgeClass(badgeStatus(entry.tunnel)), { 'status-badge-expandable': canToggleErrorDetails(entry.tunnel) }]"
                    :role="canToggleErrorDetails(entry.tunnel) ? 'button' : undefined"
                    :tabindex="canToggleErrorDetails(entry.tunnel) ? 0 : undefined"
                    :aria-label="canToggleErrorDetails(entry.tunnel) ? $t(getErrorToggleLabelKey(entry.tunnel)) : undefined"
                    :aria-expanded="canToggleErrorDetails(entry.tunnel) ? isErrorExpanded(entry.tunnel.id) : undefined"
                    @click="toggleErrorDetails(entry.tunnel)"
                    @keydown.enter.prevent="toggleErrorDetails(entry.tunnel)"
                    @keydown.space.prevent="toggleErrorDetails(entry.tunnel)"
                  >
                    <span>{{ getStatusLabelKey(badgeStatus(entry.tunnel)) ? $t(getStatusLabelKey(badgeStatus(entry.tunnel))) : badgeStatus(entry.tunnel) }}</span>
                    <i
                      v-if="canToggleErrorDetails(entry.tunnel)"
                      class="bi status-badge-toggle-icon"
                      :class="isErrorExpanded(entry.tunnel.id) ? 'bi-chevron-up' : 'bi-chevron-down'"
                    />
                  </span>
                </div>
              </td>
              <td class="tunnel-latency-cell text-muted">
                <TooltipText :text="getTunnelLatencyLabel(entry.tunnel)" :title="getTunnelLatencyTitle(entry.tunnel)" />
              </td>
              <td class="text-end tunnels-action-cell">
                <div class="btn-group btn-group-sm action-btn-group tunnel-action-menu" role="group" aria-label="Tunnel Actions">
                  <IconActionButton
                    :button-class="getPrimaryActionButtonClass(displayedStatus(entry.tunnel))"
                    :title="$t(primaryActionTitleKey(entry.tunnel))"
                    :aria-label="$t(primaryActionTitleKey(entry.tunnel))"
                    :icon-class="getPrimaryActionIcon(displayedStatus(entry.tunnel))"
                    :disabled="entry.tunnel.status === 'busy' || pendingTunnelActions.has(entry.tunnel.id)"
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
                    <li v-if="providerSetupAction(entry.tunnel)">
                      <button
                        type="button"
                        class="dropdown-item d-flex align-items-center gap-2"
                        @click="onActionSelect($event, 'provider', entry.tunnel)"
                      >
                        <i class="bi bi-box-arrow-up-right opacity-75" />
                        <span>{{ $t(providerSetupAction(entry.tunnel)) }}</span>
                      </button>
                    </li>
                    <li v-if="providerSetupAction(entry.tunnel)"><hr class="dropdown-divider" /></li>
                    <li>
                      <button
                        type="button"
                        class="dropdown-item d-flex align-items-center gap-2"
                        :disabled="entry.tunnel.status === 'busy' || pendingTunnelActions.has(entry.tunnel.id)"
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
                    <li v-if="groups.length > 0 && getMoveGroupOptions(entry.tunnel).length > 0">
                      <hr class="dropdown-divider" />
                    </li>
                    <template v-if="groups.length > 0 && getMoveGroupOptions(entry.tunnel).length > 0">
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
                          :disabled="entry.tunnel.status === 'busy' || pendingTunnelActions.has(entry.tunnel.id)"
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
                        :disabled="entry.tunnel.status === 'busy' || pendingTunnelActions.has(entry.tunnel.id)"
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
              <td colspan="6">
                <div class="tunnel-error-detail">
                  <div class="tunnel-error-detail-content">
                    <div class="tunnel-error-detail-label">{{ $t('app.tunnels.errorReason') }}</div>
                    <div class="tunnel-error-detail-message">
                      {{ errorDetailText(entry.tunnel) }}
                      <button
                        v-if="entry.tunnel.status === 'auth_required'"
                        type="button"
                        class="tunnel-error-provider-link"
                        @click="$emit('open-provider', entry.tunnel.provider || 'devtunnel')"
                      >
                        {{ $t('app.tunnels.actions.openDevTunnel') }}
                      </button>
                    </div>
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
                  <CopyIconButton
                    :value="errorDetailText(entry.tunnel)"
                    :title="$t('app.tunnels.actions.copyError')"
                    button-class="tunnel-error-copy-btn"
                  />
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </section>
</template>
