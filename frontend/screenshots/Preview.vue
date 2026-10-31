<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSidebar from '../src/components/layout/AppSidebar.vue'
import AppTopHeader from '../src/components/layout/AppTopHeader.vue'
import OverviewPage from '../src/components/pages/OverviewPage.vue'
import TunnelsPage from '../src/components/pages/TunnelsPage.vue'
import ProvidersPage from '../src/components/pages/ProvidersPage.vue'
import SyncPage from '../src/components/pages/SyncPage.vue'
import LogsPage from '../src/components/pages/LogsPage.vue'
import ConfigPage from '../src/components/pages/ConfigPage.vue'
import { frpState } from '../src/utils/frp'
import config from '../../wails.json'
import { tunnels, logs, activityLogs, jumpers, frpStatus, devTunnelStatus, devTunnelResources, cloudflareStatus, cloudflareResources, cloudflareZones, trafficHistoryUp, trafficHistoryDown } from './data'

const { t } = useI18n()
const params = new URLSearchParams(location.search)
const pageKeys = ['overview', 'providers', 'tunnels', 'sync', 'logs', 'config']
const activePage = ref(pageKeys.includes(params.get('page')) ? params.get('page') : 'overview')
const providerTab = ref(['ssh', 'devtunnel', 'cloudflare', 'frp'].includes(params.get('provider')) ? params.get('provider') : 'ssh')
const logLevel = ref('all')
const filteredLogs = computed(() => activityLogs.filter(log => logLevel.value === 'all' || log.level === logLevel.value))
const pages = computed(() => [
  ['overview', 'bi-speedometer2'], ['providers', 'bi-hdd-network'],
  ['tunnels', 'bi-diagram-3'], ['sync', 'bi-cloud-arrow-up'],
  ['logs', 'bi-journal-text'], ['config', 'bi-sliders2'],
].map(([key, icon]) => ({ key, icon, title: t(`app.sidebar.${key}`), subtitle: t(`app.sidebar.${key}Subtitle`) })))
const currentPage = computed(() => pages.value.find(page => page.key === activePage.value))
const runningTunnels = tunnels.filter(tunnel => tunnel.status === 'running')
const stoppedTunnels = tunnels.filter(tunnel => tunnel.status === 'stopped')
const pendingTunnelActions = new Map()
const describePublicTunnel = tunnel => ({ url: tunnel.publicUrl, routeBottom: tunnel.publicUrl, connection: tunnel.devTunnel?.tunnelId || tunnel.cloudflare?.tunnelId })
const getTunnelJumperLabel = () => 'Staging SSH'
Object.assign(frpState, frpStatus)

function switchPage(page) {
  if (pageKeys.includes(page)) activePage.value = page
}
</script>

<template>
  <div class="app-root" data-screenshot-preview="true">
    <div class="app-shell">
      <AppSidebar :pages="pages" :active-page="activePage" :app-version="config.info.productVersion"
        :is-pro="true" pro-expiry-label="2027-12-31" :show-upgrade-button="true"
        :traffic-monitor-enabled="activePage === 'overview'"
        :traffic="{ upBps: 47104, downBps: 131072 }" :traffic-history-up="trafficHistoryUp"
        :traffic-history-down="trafficHistoryDown" @switch-page="switchPage" />
      <section class="content-shell">
        <AppTopHeader :current-page="currentPage" :active-page="activePage" :class="{ 'providers-top-header': activePage === 'providers' }" />
        <main class="page-body" :class="{ 'page-body-overview': activePage === 'overview' }">
          <OverviewPage v-if="activePage === 'overview'" :tunnels="tunnels" :total-tunnels="tunnels.length"
            :running-tunnels="runningTunnels" :stopped-tunnels="stoppedTunnels" :auto-start-count="2"
            :pending-tunnel-actions="pendingTunnelActions" :logs="logs" :get-tunnel-jumper-label="getTunnelJumperLabel"
            :describe-dev-tunnel="describePublicTunnel" :describe-cloudflare="describePublicTunnel" @open-tunnels="switchPage('tunnels')" />
          <TunnelsPage v-else-if="activePage === 'tunnels'" :tunnels="tunnels" :pending-tunnel-actions="pendingTunnelActions"
            :mode-options="[]" :tunnel-ai-debug-states="{}" :get-tunnel-jumper-label="getTunnelJumperLabel"
            :describe-dev-tunnel="describePublicTunnel" :describe-cloudflare="describePublicTunnel" />
          <ProvidersPage v-else-if="activePage === 'providers'" v-model:tab="providerTab" :jumpers="jumpers" :jumper-total="jumpers.length" :tunnels="tunnels"
            :dev-tunnel-status="devTunnelStatus" :dev-tunnel-resources="devTunnelResources"
            :cloudflare-status="cloudflareStatus" :cloudflare-resources="cloudflareResources" :cloudflare-zones="cloudflareZones" />
          <SyncPage v-else-if="activePage === 'sync'" />
          <LogsPage v-else-if="activePage === 'logs'" :selected-log-level="logLevel" :filtered-logs="filteredLogs" @set-log-level="logLevel = $event" />
          <ConfigPage v-else-if="activePage === 'config'" theme="light" accent-color="blue" :app-meta="{ version: config.info.productVersion }"
            :is-pro="true" pro-expiry-label="2027-12-31" license-code="DEMO-SCREENSHOT-ONLY"
            :update-check-dialog="{ visible: false }" />
        </main>
      </section>
    </div>
  </div>
</template>
