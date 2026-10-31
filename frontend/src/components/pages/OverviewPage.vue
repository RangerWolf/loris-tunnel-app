<script setup>
import { latencyLabel, latencyTitle, formatLatencyLabel } from '../../utils/tunnel-latency'
import IconActionButton from '../common/IconActionButton.vue'
import SoftActionButton from '../common/SoftActionButton.vue'
import TooltipText from '../common/TooltipText.vue'
import LogEntriesTable from '../common/LogEntriesTable.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  pendingTunnelActions: { type: Map, required: true },
  tunnels: { type: Array, default: () => [] },
  totalTunnels: { type: Number, required: true },
  runningTunnels: { type: Array, required: true },
  stoppedTunnels: { type: Array, required: true },
  autoStartCount: { type: Number, required: true },
  logs: { type: Array, required: true },
  getTunnelJumperLabel: { type: Function, required: true },
  describeDevTunnel: { type: Function, default: null },
  describeCloudflare: { type: Function, default: null }
})

const emit = defineEmits(['toggle-tunnel', 'open-tunnels'])
const { t } = useI18n()
const page = ref(1)
const PAGE_SIZE = 5

const allTunnels = computed(() => props.tunnels.length
  ? props.tunnels
  : [...props.runningTunnels, ...props.stoppedTunnels])
const errorTunnels = computed(() => allTunnels.value.filter((tunnel) => tunnel.status === 'error'))
const stoppedCount = computed(() => allTunnels.value.filter((tunnel) => tunnel.status === 'stopped').length)
const pageCount = computed(() => Math.max(1, Math.ceil(props.runningTunnels.length / PAGE_SIZE)))
const visibleTunnels = computed(() => props.runningTunnels.slice((page.value - 1) * PAGE_SIZE, page.value * PAGE_SIZE))

watch(pageCount, (value) => { if (page.value > value) page.value = value })

function tunnelTypeLabel(tunnel) {
  const provider = tunnel?.provider || 'ssh'
  const providerKey = `app.providers.${provider}`
  const providerText = t(providerKey)
  const providerLabel = providerText && providerText !== providerKey ? providerText : provider
  const mode = tunnel?.mode || 'local'
  const modeKey = `app.options.mode.${mode}`
  const modeText = t(modeKey)
  const modeLabel = (modeText && modeText !== modeKey ? modeText : mode).replace(/\s*\([^)]*\)\s*$/, '')
  return `${providerLabel} - ${modeLabel}`
}

function routeLines(tunnel) {
  const local = `${tunnel.localHost}:${tunnel.localPort}`
  const provider = tunnel.provider || 'ssh'
  if (provider === 'frp') return { top: local, bottom: tunnel.publicUrl || t('app.frp.missingServer'), title: tunnel.publicUrl || '' }
  if (provider === 'devtunnel' || provider === 'cloudflare') {
    const describe = provider === 'devtunnel' ? props.describeDevTunnel : props.describeCloudflare
    const info = typeof describe === 'function' ? describe(tunnel) : null
    const url = info?.url || (provider === 'cloudflare' && tunnel.cloudflare?.hostname ? `https://${tunnel.cloudflare.hostname}` : '')
    return { top: local, bottom: url || info?.routeBottom || t('app.tunnels.waitingForUrl'), title: info?.routeTitle || url }
  }
  const remote = `${tunnel.remoteHost}:${tunnel.remotePort}`
  if (tunnel.mode === 'dynamic') return { top: local, bottom: 'SOCKS5' }
  return tunnel.mode === 'remote' ? { top: remote, bottom: local } : { top: local, bottom: remote }
}

</script>

<template>
  <section class="page-fade lt-overview">
    <div class="lt-overview-metrics">
      <div class="metric-card">
        <div class="metric-label"><span class="lt-status-dot running" aria-hidden="true" />{{ $t('app.overview.running') }}</div>
        <div class="metric-value text-success-emphasis">{{ runningTunnels.length }}</div>
      </div>
      <div class="metric-card">
        <div class="metric-label"><span class="lt-status-dot error" aria-hidden="true" />{{ $t('app.overview.failed') }}</div>
        <div class="metric-value" :class="{ 'text-danger-emphasis': errorTunnels.length }">{{ errorTunnels.length }}</div>
      </div>
      <div class="metric-card">
        <div class="metric-label"><span class="lt-status-dot stopped" aria-hidden="true" />{{ $t('app.overview.stopped') }}</div>
        <div class="metric-value">{{ stoppedCount }}</div>
      </div>
      <div class="metric-card">
        <div class="metric-label"><span class="lt-status-dot auto-start" aria-hidden="true" />{{ $t('app.overview.autoStart') }}</div>
        <div class="metric-value">{{ autoStartCount }}</div>
      </div>
    </div>

    <div class="panel-card lt-overview-list">
      <div class="panel-head">
        <span class="panel-title">{{ $t('app.overview.activeTunnels') }}</span>
        <SoftActionButton @click="emit('open-tunnels')">
          {{ $t('app.overview.manageTunnels') }} <i class="bi bi-arrow-right-short" aria-hidden="true" />
        </SoftActionButton>
      </div>
      <div class="lt-overview-list-body">
        <div class="table-responsive">
          <table class="table align-middle mb-0 lt-overview-tunnel-table">
            <thead>
              <tr>
                <th>{{ $t('app.overview.table.name') }}</th>
                <th>{{ $t('app.overview.table.route') }}</th>
                <th>{{ $t('app.tunnels.table.latency') }}</th>
                <th class="text-end">{{ $t('app.overview.table.action') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!runningTunnels.length" class="lt-overview-empty-row"><td colspan="4" class="text-muted py-4">{{ $t('app.overview.noRunningTunnels') }}</td></tr>
              <tr v-for="tunnel in visibleTunnels" :key="tunnel.id">
                <td class="fw-semibold">
                  <TooltipText :text="tunnel.name" />
                  <span class="lt-tunnel-secondary lt-tunnel-type-sub">{{ tunnelTypeLabel(tunnel) }}</span>
                </td>
                <td class="lt-overview-route">
                  <div class="tunnel-route-wrap">
                    <TooltipText :text="routeLines(tunnel).top" class-name="cell-ellipsis route-line" />
                    <span class="route-line route-line-secondary d-flex align-items-center min-w-0">
                      <TooltipText :text="routeLines(tunnel).bottom" :title="routeLines(tunnel).title" class-name="cell-ellipsis" />
                    </span>
                  </div>
                </td>
                <td class="text-muted"><TooltipText :text="latencyLabel(tunnel, t, formatLatencyLabel)" :title="latencyTitle(tunnel, t)" /></td>
                <td class="text-end">
                  <IconActionButton
                    button-class="btn-outline-danger"
                    :title="$t(pendingTunnelActions.has(tunnel.id) ? 'app.tunnels.actions.busy' : 'app.overview.actions.stopTunnel')"
                    :aria-label="$t(pendingTunnelActions.has(tunnel.id) ? 'app.tunnels.actions.busy' : 'app.overview.actions.stopTunnel')"
                    :icon-class="pendingTunnelActions.has(tunnel.id) ? 'bi-arrow-repeat spin' : 'bi-pause'"
                    :disabled="pendingTunnelActions.has(tunnel.id)"
                    @click="emit('toggle-tunnel', tunnel)"
                  />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="pageCount > 1" class="lt-overview-pagination">
          <span>{{ $t('app.overview.pagination.pageInfo', { current: page, total: pageCount }) }}</span>
          <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="page === 1" :aria-label="$t('app.overview.pagination.prev')" @click="page--">
            <i class="bi bi-chevron-left" aria-hidden="true" />
          </button>
          <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="page === pageCount" :aria-label="$t('app.overview.pagination.next')" @click="page++">
            <i class="bi bi-chevron-right" aria-hidden="true" />
          </button>
        </div>
      </div>
    </div>

    <div class="panel-card lt-overview-activity">
      <div class="panel-head">
        <span class="panel-title">{{ $t('app.overview.recentActivity') }}</span>
      </div>
      <div>
        <LogEntriesTable :entries="logs.slice(0, 6)" show-empty />
      </div>
    </div>
  </section>
</template>
