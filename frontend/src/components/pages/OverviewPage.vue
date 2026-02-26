<script setup>
import IconActionButton from '../common/IconActionButton.vue'
import { getLogLevelClass } from '../../utils/log-level-class'
import { computed, ref, watch } from 'vue'

const props = defineProps({
  totalTunnels: {
    type: Number,
    required: true
  },
  runningTunnels: {
    type: Array,
    required: true
  },
  stoppedTunnels: {
    type: Array,
    required: true
  },
  autoStartCount: {
    type: Number,
    required: true
  },
  showOverviewActive: {
    type: Boolean,
    required: true
  },
  showOverviewActivity: {
    type: Boolean,
    required: true
  },
  logs: {
    type: Array,
    required: true
  },
  getTunnelJumperLabel: {
    type: Function,
    required: true
  }
})

defineEmits(['toggle-overview-active', 'toggle-overview-activity', 'toggle-tunnel'])

const ACTIVE_PAGE_SIZE = 5
const activeTunnelsPage = ref(1)

const activeTunnelsTotalPages = computed(() => {
  return Math.max(1, Math.ceil(props.runningTunnels.length / ACTIVE_PAGE_SIZE))
})

const pagedRunningTunnels = computed(() => {
  const start = (activeTunnelsPage.value - 1) * ACTIVE_PAGE_SIZE
  return props.runningTunnels.slice(start, start + ACTIVE_PAGE_SIZE)
})

const showActiveTunnelsPagination = computed(() => props.runningTunnels.length > ACTIVE_PAGE_SIZE)

watch(
  () => props.runningTunnels.length,
  () => {
    if (!showActiveTunnelsPagination.value) {
      activeTunnelsPage.value = 1
      return
    }
    if (activeTunnelsPage.value > activeTunnelsTotalPages.value) {
      activeTunnelsPage.value = activeTunnelsTotalPages.value
    }
  }
)

function goPrevActiveTunnelsPage() {
  if (activeTunnelsPage.value <= 1) return
  activeTunnelsPage.value -= 1
}

function goNextActiveTunnelsPage() {
  if (activeTunnelsPage.value >= activeTunnelsTotalPages.value) return
  activeTunnelsPage.value += 1
}

function getOverviewRoute(tunnel) {
  return `${tunnel.localHost}:${tunnel.localPort} -> ${tunnel.remoteHost}:${tunnel.remotePort}`
}
</script>

<template>
  <section class="page-fade">
    <div class="row g-3">
      <div class="col-6 col-xl-3">
        <div class="metric-card">
          <div class="metric-label">{{ $t('app.overview.totalTunnels') }}</div>
          <div class="metric-value">{{ totalTunnels }}</div>
        </div>
      </div>
      <div class="col-6 col-xl-3">
        <div class="metric-card">
          <div class="metric-label">{{ $t('app.overview.running') }}</div>
          <div class="metric-value text-success-emphasis">{{ runningTunnels.length }}</div>
        </div>
      </div>
      <div class="col-6 col-xl-3">
        <div class="metric-card">
          <div class="metric-label">{{ $t('app.overview.stopped') }}</div>
          <div class="metric-value text-secondary-emphasis">{{ stoppedTunnels.length }}</div>
        </div>
      </div>
      <div class="col-6 col-xl-3">
        <div class="metric-card">
          <div class="metric-label">{{ $t('app.overview.autoStart') }}</div>
          <div class="metric-value">{{ autoStartCount }}</div>
        </div>
      </div>
    </div>

    <div class="row g-3 mt-1">
      <div class="col-12 col-xl-7">
        <div class="panel-card overview-panel overview-panel-active" :class="{ 'overview-panel-collapsed': !showOverviewActive }">
          <div class="panel-head">
            <button
              type="button"
              class="overview-title-btn"
              @click="$emit('toggle-overview-active')"
              @keydown.enter.prevent="$emit('toggle-overview-active')"
              @keydown.space.prevent="$emit('toggle-overview-active')"
            >
              <span class="panel-title">{{ $t('app.overview.activeTunnels') }}</span>
            </button>
            <button
              type="button"
              class="btn btn-sm btn-link overview-toggle"
              :aria-label="showOverviewActive ? $t('app.overview.collapsePanel') : $t('app.overview.expandPanel')"
              :aria-expanded="showOverviewActive"
              @click="$emit('toggle-overview-active')"
            >
              <i class="bi" :class="showOverviewActive ? 'bi-chevron-up' : 'bi-chevron-down'" />
            </button>
          </div>
          <div v-show="showOverviewActive" class="overview-panel-body overview-panel-body-table">
            <div class="overview-scroll overview-scroll-no-y table-responsive">
              <table class="table align-middle mb-0 overview-active-table">
                <thead>
                  <tr>
                    <th>{{ $t('app.overview.table.name') }}</th>
                    <th>{{ $t('app.overview.table.route') }}</th>
                    <th>{{ $t('app.overview.table.jumper') }}</th>
                    <th class="text-end">{{ $t('app.overview.table.action') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="runningTunnels.length === 0">
                    <td colspan="4" class="text-muted py-4">{{ $t('app.overview.noRunningTunnels') }}</td>
                  </tr>
                  <tr v-for="tunnel in pagedRunningTunnels" :key="tunnel.id">
                    <td class="fw-semibold tunnel-name-cell">
                      <span class="cell-ellipsis" :title="tunnel.name">{{ tunnel.name }}</span>
                    </td>
                    <td class="text-muted overview-route-cell">
                      <span class="cell-ellipsis" :title="getOverviewRoute(tunnel)">{{ getOverviewRoute(tunnel) }}</span>
                    </td>
                    <td class="overview-jumper-cell">
                      <span class="cell-ellipsis" :title="getTunnelJumperLabel(tunnel)">{{ getTunnelJumperLabel(tunnel) }}</span>
                    </td>
                    <td class="text-end">
                      <IconActionButton
                        button-class="btn btn-outline-danger"
                        :title="$t('app.overview.actions.stopTunnel')"
                        :aria-label="$t('app.overview.actions.stopTunnel')"
                        icon-class="bi-pause"
                        @click="$emit('toggle-tunnel', tunnel)"
                      />
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-if="showActiveTunnelsPagination" class="overview-pagination">
              <button
                type="button"
                class="btn btn-sm btn-outline-secondary"
                :aria-label="$t('app.overview.pagination.prev')"
                :disabled="activeTunnelsPage === 1"
                @click="goPrevActiveTunnelsPage"
              >
                <i class="bi bi-chevron-left" />
              </button>
              <span class="overview-pagination-info">
                {{ $t('app.overview.pagination.pageInfo', { current: activeTunnelsPage, total: activeTunnelsTotalPages }) }}
              </span>
              <button
                type="button"
                class="btn btn-sm btn-outline-secondary"
                :aria-label="$t('app.overview.pagination.next')"
                :disabled="activeTunnelsPage === activeTunnelsTotalPages"
                @click="goNextActiveTunnelsPage"
              >
                <i class="bi bi-chevron-right" />
              </button>
            </div>
          </div>
        </div>
      </div>

      <div class="col-12 col-xl-5">
        <div class="panel-card overview-panel" :class="{ 'overview-panel-collapsed': !showOverviewActivity }">
          <div class="panel-head">
            <button
              type="button"
              class="overview-title-btn"
              @click="$emit('toggle-overview-activity')"
              @keydown.enter.prevent="$emit('toggle-overview-activity')"
              @keydown.space.prevent="$emit('toggle-overview-activity')"
            >
              <span class="panel-title">{{ $t('app.overview.recentActivity') }}</span>
            </button>
            <button
              type="button"
              class="btn btn-sm btn-link overview-toggle"
              :aria-label="showOverviewActivity ? $t('app.overview.collapsePanel') : $t('app.overview.expandPanel')"
              :aria-expanded="showOverviewActivity"
              @click="$emit('toggle-overview-activity')"
            >
              <i class="bi" :class="showOverviewActivity ? 'bi-chevron-up' : 'bi-chevron-down'" />
            </button>
          </div>
          <div v-show="showOverviewActivity" class="overview-panel-body">
            <div class="overview-scroll">
              <div class="activity-list">
                <div v-for="entry in logs.slice(0, 6)" :key="entry.id" class="activity-row">
                  <span class="activity-time">{{ entry.time }}</span>
                  <span
                    class="activity-level"
                    :class="getLogLevelClass(entry.level, 'activityLevel')"
                  >
                    {{ entry.level.toUpperCase() }}
                  </span>
                  <span class="activity-message">{{ entry.message }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
