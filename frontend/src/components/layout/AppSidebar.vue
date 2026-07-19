<script setup>
import { computed, ref } from 'vue'

const SPARKLINE_WIDTH = 200
const SPARKLINE_HEIGHT = 28

const props = defineProps({
  pages: {
    type: Array,
    required: true
  },
  activePage: {
    type: String,
    required: true
  },
  appVersion: {
    type: String,
    required: true
  },
  isPro: {
    type: Boolean,
    required: true
  },
  proExpiryLabel: {
    type: String,
    required: true
  },
  hasNewVersion: {
    type: Boolean,
    default: false
  },
  collapsed: {
    type: Boolean,
    default: false
  },
  trafficMonitorEnabled: {
    type: Boolean,
    default: true
  },
  traffic: {
    type: Object,
    default: () => ({ upBps: 0, downBps: 0 })
  },
  trafficHistoryUp: {
    type: Array,
    default: () => []
  },
  trafficHistoryDown: {
    type: Array,
    default: () => []
  }
})

defineEmits(['switch-page', 'upgrade', 'open-release-page', 'toggle-collapse'])

const chartRef = ref(null)
const hoverIndex = ref(null)
const hoverX = ref(0)

function formatBytesRate(bps) {
  const value = Math.max(0, Number(bps) || 0)
  if (value < 1024) return `${Math.round(value)} B/s`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(2)} KB/s`
  return `${(value / (1024 * 1024)).toFixed(2)} MB/s`
}

function historyPeak(history) {
  const data = Array.isArray(history) ? history : []
  if (data.length === 0) return 0
  return Math.max(...data.map((value) => Math.max(0, Number(value) || 0)), 0)
}

function buildSparklinePoints(history, width, height, scaleMax) {
  const data = Array.isArray(history) ? history : []
  if (data.length === 0) {
    return `0,${height} ${width},${height}`
  }
  const max = Math.max(scaleMax, 1)
  const stepX = data.length <= 1 ? width : width / (data.length - 1)
  return data
    .map((value, index) => {
      const x = (stepX * index).toFixed(1)
      const y = (height - (value / max) * (height - 2) - 1).toFixed(1)
      return `${x},${y}`
    })
    .join(' ')
}

const historyLength = computed(() => Math.max(props.trafficHistoryUp.length, props.trafficHistoryDown.length))

const chartScaleMax = computed(() => {
  const upPeak = historyPeak(props.trafficHistoryUp)
  const downPeak = historyPeak(props.trafficHistoryDown)
  return Math.max(upPeak, downPeak, 1)
})

const uploadSparkPoints = computed(() => buildSparklinePoints(
  props.trafficHistoryUp,
  SPARKLINE_WIDTH,
  SPARKLINE_HEIGHT,
  chartScaleMax.value
))
const downloadSparkPoints = computed(() => buildSparklinePoints(
  props.trafficHistoryDown,
  SPARKLINE_WIDTH,
  SPARKLINE_HEIGHT,
  chartScaleMax.value
))

const hoverUploadBps = computed(() => {
  if (hoverIndex.value === null) return 0
  return Math.max(0, Number(props.trafficHistoryUp[hoverIndex.value]) || 0)
})

const hoverDownloadBps = computed(() => {
  if (hoverIndex.value === null) return 0
  return Math.max(0, Number(props.trafficHistoryDown[hoverIndex.value]) || 0)
})

const hoverLineX = computed(() => {
  if (hoverIndex.value === null || historyLength.value <= 1) return 0
  return (hoverIndex.value / (historyLength.value - 1)) * SPARKLINE_WIDTH
})

const tooltipLeft = computed(() => {
  const width = chartRef.value?.clientWidth ?? 0
  const half = 52
  if (width <= half * 2) return width / 2
  return Math.min(Math.max(hoverX.value, half), width - half)
})

function resolveHistoryIndex(clientX) {
  const el = chartRef.value
  if (!el) return null
  const len = historyLength.value
  if (len === 0) return null
  if (len === 1) return 0

  const rect = el.getBoundingClientRect()
  const ratio = Math.min(Math.max((clientX - rect.left) / rect.width, 0), 1)
  return Math.round(ratio * (len - 1))
}

function onChartMouseMove(event) {
  const index = resolveHistoryIndex(event.clientX)
  if (index === null) {
    hoverIndex.value = null
    return
  }
  hoverIndex.value = index
  const rect = chartRef.value.getBoundingClientRect()
  hoverX.value = event.clientX - rect.left
}

function onChartMouseLeave() {
  hoverIndex.value = null
}
</script>

<template>
  <aside class="sidebar-panel" :class="{ collapsed }">
    <div class="sidebar-main">
      <div class="brand-logo-wrap">
        <div class="brand-meta">
          <div class="brand-title">{{ $t('app.title') }}</div>
          <div class="brand-subtitle">{{ $t('app.sidebar.subtitle') }}</div>
        </div>
        <button
          type="button"
          class="btn sidebar-collapse-btn"
          :title="collapsed ? $t('app.sidebar.expand') : $t('app.sidebar.collapse')"
          :aria-label="collapsed ? $t('app.sidebar.expand') : $t('app.sidebar.collapse')"
          @click="$emit('toggle-collapse')"
        >
          <i class="bi" :class="collapsed ? 'bi-layout-sidebar-inset' : 'bi-layout-sidebar'" aria-hidden="true" />
        </button>
      </div>

      <div class="nav-list mt-4">
        <button
          v-for="page in pages"
          :key="page.key"
          type="button"
          class="btn nav-item-btn"
          :class="{ active: activePage === page.key }"
          :title="collapsed ? page.title : ''"
          :aria-label="page.title"
          @click="$emit('switch-page', page.key)"
        >
          <i class="bi nav-item-icon" :class="page.icon" aria-hidden="true" />
          <span class="nav-item-label">
            <span v-if="!collapsed">{{ page.title }}</span>
            <span v-if="page.beta" class="nav-beta-tag">Beta</span>
          </span>
        </button>
      </div>
    </div>

    <div class="sidebar-bottom">
      <div
        v-if="trafficMonitorEnabled"
        class="sidebar-traffic"
        :class="{ collapsed }"
        :aria-label="$t('app.sidebar.traffic')"
      >
        <div
          v-if="!collapsed"
          ref="chartRef"
          class="traffic-chart-wrap"
          @mousemove="onChartMouseMove"
          @mouseleave="onChartMouseLeave"
        >
          <svg
            class="traffic-sparkline"
            :viewBox="`0 0 ${SPARKLINE_WIDTH} ${SPARKLINE_HEIGHT}`"
            preserveAspectRatio="none"
            aria-hidden="true"
          >
            <polyline class="traffic-line-down" :points="downloadSparkPoints" />
            <polyline class="traffic-line-up" :points="uploadSparkPoints" />
            <line
              v-if="hoverIndex !== null"
              class="traffic-hover-line"
              :x1="hoverLineX"
              :x2="hoverLineX"
              y1="0"
              :y2="SPARKLINE_HEIGHT"
            />
          </svg>
          <div
            v-if="hoverIndex !== null"
            class="traffic-chart-tooltip"
            :style="{ left: `${tooltipLeft}px` }"
          >
            <div class="traffic-tooltip-row traffic-rate-up">
              <i class="bi bi-arrow-up-short" aria-hidden="true" />
              <span>{{ formatBytesRate(hoverUploadBps) }}</span>
            </div>
            <div class="traffic-tooltip-row traffic-rate-down">
              <i class="bi bi-arrow-down-short" aria-hidden="true" />
              <span>{{ formatBytesRate(hoverDownloadBps) }}</span>
            </div>
          </div>
        </div>

        <div class="traffic-rates">
          <div class="traffic-rate-row traffic-rate-up">
            <i class="bi bi-arrow-up-short traffic-rate-icon" aria-hidden="true" />
            <span class="traffic-rate-current" :title="$t('app.sidebar.upload')">
              {{ formatBytesRate(traffic.upBps) }}
            </span>
          </div>
          <div class="traffic-rate-row traffic-rate-down">
            <i class="bi bi-arrow-down-short traffic-rate-icon" aria-hidden="true" />
            <span class="traffic-rate-current" :title="$t('app.sidebar.download')">
              {{ formatBytesRate(traffic.downBps) }}
            </span>
          </div>
        </div>
      </div>

      <div class="sidebar-footer">
        <span class="sidebar-version" :class="{ compact: collapsed }">
          <template v-if="!collapsed">v{{ appVersion }}</template>
          <template v-else>v</template>
          <span v-if="hasNewVersion" class="version-new-badge" @click="$emit('open-release-page')">new</span>
        </span>
        <button
          v-if="!isPro"
          type="button"
          class="btn btn-primary btn-sm sidebar-upgrade-btn"
          :title="$t('app.sidebar.upgrade')"
          :aria-label="$t('app.sidebar.upgrade')"
          @click="$emit('upgrade')"
        >
          <template v-if="collapsed">
            <i class="bi bi-stars" aria-hidden="true" />
          </template>
          <template v-else>
            {{ $t('app.sidebar.upgrade') }}
          </template>
        </button>
        <template v-else>
          <span
            v-if="collapsed"
            class="sidebar-pro-badge"
            :title="$t('config.proExpires', { date: proExpiryLabel })"
            :aria-label="$t('config.proExpires', { date: proExpiryLabel })"
          >
            <i class="bi bi-patch-check-fill" aria-hidden="true" />
          </span>
          <span
            v-else
            class="sidebar-pro-expiry"
            :title="$t('config.proExpires', { date: proExpiryLabel })"
          >
            Pro · {{ proExpiryLabel }}
          </span>
        </template>
      </div>
    </div>
  </aside>
</template>
