<script setup>
import IconActionButton from '../common/IconActionButton.vue'
import TooltipText from '../common/TooltipText.vue'
import { Dropdown } from 'bootstrap'
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  tunnels: {
    type: Array,
    required: true
  },
  searchQuery: {
    type: String,
    default: ''
  },
  modeOptions: {
    type: Array,
    required: true
  },
  getTunnelJumperLabel: {
    type: Function,
    required: true
  }
})

const emit = defineEmits(['toggle-tunnel', 'copy-tunnel', 'edit-tunnel', 'delete-tunnel', 'update-search-query'])
const { t } = useI18n()

const rootRef = ref(null)
const localSearchQuery = ref(props.searchQuery)
const expandedErrorIds = ref(new Set())
const copiedErrorIds = ref(new Set())
const copyResetTimers = new Map()
const ACTION_DROPDOWN_READY_ATTR = 'data-lt-action-dropdown-ready'

watch(() => props.searchQuery, (newValue) => {
  localSearchQuery.value = newValue
})

watch(localSearchQuery, (newValue) => {
  emit('update-search-query', newValue)
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
function onActionSelect(event, action, tunnel) {
  hideActionDropdown(event)
  if (action === 'edit') {
    emit('edit-tunnel', tunnel)
    return
  }
  if (action === 'copy') {
    emit('copy-tunnel', tunnel)
    return
  }
  emit('delete-tunnel', tunnel)
}
</script>

<template>
  <section ref="rootRef" class="page-fade panel-card">
    <div class="panel-head">
      <h2 class="panel-title mb-0">{{ $t('app.tunnels.title') }}</h2>
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
          <tr v-if="tunnels.length === 0">
            <td colspan="7" class="text-muted py-4">{{ $t('app.tunnels.noTunnels') }}</td>
          </tr>
          <template v-for="tunnel in tunnels" :key="`tunnel-${tunnel.id}`">
            <tr>
              <td class="fw-semibold tunnel-name-cell">
                <TooltipText :text="tunnel.name" class-name="cell-ellipsis" />
              </td>
              <td class="tunnel-mode-cell">{{ getModeLabel(tunnel.mode) }}</td>
              <td class="text-muted tunnel-route-cell">
                <div class="tunnel-route-wrap">
                  <TooltipText :text="getRouteTop(tunnel)" class-name="cell-ellipsis route-line" />
                  <TooltipText :text="getRouteBottom(tunnel)" class-name="cell-ellipsis route-line route-line-secondary" />
                </div>
              </td>
              <td class="tunnel-jumper-cell">
                <TooltipText :text="getTunnelJumperLabel(tunnel)" class-name="cell-ellipsis" />
              </td>
              <td class="tunnel-status-cell">
                <div class="tunnel-status-wrap">
                  <span
                    class="status-badge"
                    :class="[getStatusBadgeClass(tunnel.status), { 'status-badge-expandable': canToggleErrorDetails(tunnel) }]"
                    :role="canToggleErrorDetails(tunnel) ? 'button' : undefined"
                    :tabindex="canToggleErrorDetails(tunnel) ? 0 : undefined"
                    :aria-label="canToggleErrorDetails(tunnel) ? $t(getErrorToggleLabelKey(tunnel)) : undefined"
                    :aria-expanded="canToggleErrorDetails(tunnel) ? isErrorExpanded(tunnel.id) : undefined"
                    @click="toggleErrorDetails(tunnel)"
                    @keydown.enter.prevent="toggleErrorDetails(tunnel)"
                    @keydown.space.prevent="toggleErrorDetails(tunnel)"
                  >
                    <span>{{ getStatusLabelKey(tunnel.status) ? $t(getStatusLabelKey(tunnel.status)) : tunnel.status }}</span>
                    <i
                      v-if="canToggleErrorDetails(tunnel)"
                      class="bi status-badge-toggle-icon"
                      :class="isErrorExpanded(tunnel.id) ? 'bi-chevron-up' : 'bi-chevron-down'"
                    />
                  </span>
                </div>
              </td>
              <td class="tunnel-latency-cell text-muted">
                <span class="cell-ellipsis" :title="getTunnelLatencyLabel(tunnel)">{{ getTunnelLatencyLabel(tunnel) }}</span>
              </td>
              <td class="text-end tunnels-action-cell">
                <div class="btn-group btn-group-sm action-btn-group tunnel-action-menu" role="group" aria-label="Tunnel Actions">
                  <IconActionButton
                    :button-class="getPrimaryActionButtonClass(tunnel.status)"
                    :title="$t(getPrimaryActionTitle(tunnel.status))"
                    :aria-label="$t(getPrimaryActionTitle(tunnel.status))"
                    :icon-class="getPrimaryActionIcon(tunnel.status)"
                    :disabled="tunnel.status === 'busy'"
                    @click="$emit('toggle-tunnel', tunnel)"
                  />
                  <button
                    type="button"
                    class="btn icon-btn action-menu-toggle dropdown-toggle dropdown-toggle-split"
                    :class="getMenuToggleButtonClass(tunnel.status)"
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
                        :disabled="tunnel.status === 'busy'"
                        @click="onActionSelect($event, 'edit', tunnel)"
                      >
                        <i class="bi bi-sliders opacity-75" />
                        <span>{{ $t('app.tunnels.actions.edit') }}</span>
                      </button>
                    </li>
                    <li>
                      <button
                        type="button"
                        class="dropdown-item d-flex align-items-center gap-2"
                        @click="onActionSelect($event, 'copy', tunnel)"
                      >
                        <i class="bi bi-copy opacity-75" />
                        <span>{{ $t('app.tunnels.actions.copy') }}</span>
                      </button>
                    </li>
                    <li><hr class="dropdown-divider" /></li>
                    <li>
                      <button
                        type="button"
                        class="dropdown-item d-flex align-items-center gap-2 text-danger"
                        :disabled="tunnel.status === 'busy'"
                        @click="onActionSelect($event, 'delete', tunnel)"
                      >
                        <i class="bi bi-trash3" />
                        <span>{{ $t('app.tunnels.actions.delete') }}</span>
                      </button>
                    </li>
                  </ul>
                </div>
              </td>
            </tr>
            <tr v-if="isErrorExpanded(tunnel.id)" class="tunnel-error-detail-row">
              <td colspan="7">
                <div class="tunnel-error-detail">
                  <div class="tunnel-error-detail-content">
                    <div class="tunnel-error-detail-label">{{ $t('app.tunnels.errorReason') }}</div>
                    <div class="tunnel-error-detail-message">{{ tunnel.lastError }}</div>
                  </div>
                  <button
                    type="button"
                    class="btn btn-sm tunnel-error-copy-btn"
                    :class="{ 'is-copied': isErrorCopied(tunnel.id) }"
                    :aria-label="$t(getErrorCopyLabelKey(tunnel.id))"
                    :title="$t(getErrorCopyLabelKey(tunnel.id))"
                    @click="copyErrorDetails(tunnel)"
                  >
                    <i class="bi" :class="isErrorCopied(tunnel.id) ? 'bi-check2' : 'bi-copy'" />
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
