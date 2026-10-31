<script setup>
import { nextTick, ref } from 'vue'
import JumpersPage from './JumpersPage.vue'
import DevTunnelProvider from '../providers/DevTunnelProvider.vue'
import FRPProvider from '../providers/FRPProvider.vue'
import CloudflareProvider from '../providers/CloudflareProvider.vue'
import { providerCapabilities } from '../../config/features'

const props = defineProps({
  frpDraftPending: { type: Boolean, default: false },
  tab: { type: String, default: 'ssh' },
  jumpers: { type: Array, required: true },
  jumperTotal: { default: null },
  searchQuery: { type: String, default: '' },
  devTunnelStatus: { type: Object, default: () => ({}) },
  devTunnelBusy: { type: String, default: '' },
  devTunnelResources: { type: Object, default: () => ({ state: 'idle', detail: '', items: [] }) },
  tunnels: { type: Array, default: () => [] },
  devTunnelResourceBusy: { type: String, default: '' },
  devTunnelCreated: { type: Object, default: null },
  devTunnelActionResult: { type: Object, default: null },
  devTunnelDetail: { type: Object, default: null },
  devTunnelCreateError: { type: String, default: '' },
  devTunnelRegionProbe: { type: Object, default: () => ({ state: 'idle', regions: [], loading: false }) },
  cloudflareStatus: { type: Object, default: () => ({}) },
  cloudflareBusy: { type: String, default: '' },
  cloudflareResources: { type: Object, default: () => ({ state: 'idle', detail: '', items: [] }) },
  cloudflareResourceBusy: { type: String, default: '' },
  cloudflareCreated: { type: Object, default: null },
  cloudflareActionResult: { type: Object, default: null },
  cloudflareDetail: { type: Object, default: null },
  cloudflareCreateError: { type: String, default: '' },
  cloudflareZones: { type: Object, default: () => ({ state: 'idle', detail: '', items: [] }) },
})

const emit = defineEmits([
  'resume-frp-draft',
  'create-frp-tunnel',
  'view-frp-tunnels',
  'update-search-query',
  'import-jumper',
  'new-jumper',
  'copy-jumper',
  'edit-jumper',
  'delete-jumper',
  'refresh-devtunnel',
  'install-devtunnel',
  'cancel-devtunnel-install',
  'login-devtunnel',
  'cancel-devtunnel-login',
  'logout-devtunnel',
  'open-devtunnel-login',
  'open-devtunnel-docs',
  'update:tab',
  'refresh-devtunnel-resources',
  'create-devtunnel-resource',
  'delete-devtunnel-resource',
  'retry-devtunnel-cleanup',
  'create-devtunnel',
  'inspect-devtunnel-resource',
  'open-url',
  'reset-devtunnel-created',
  'load-devtunnel-regions',
  'test-devtunnel-regions',
  'refresh-cloudflare',
  'install-cloudflare',
  'cancel-cloudflare-install',
  'login-cloudflare',
  'cloudflare-authorizations-changed',
  'cancel-cloudflare-login',
  'open-cloudflare-login',
  'open-cloudflare-docs',
  'refresh-cloudflare-resources',
  'refresh-cloudflare-zones',
  'create-cloudflare-resource',
  'delete-cloudflare-resource',
  'retry-cloudflare-cleanup',
  'create-cloudflare',
  'inspect-cloudflare-resource',
  'reset-cloudflare-created',
])

const devSearchQuery = ref('')
const cloudflareSearchQuery = ref('')
const tabs = ['ssh', 'devtunnel', 'cloudflare', 'frp'].filter((id) => providerCapabilities[id]?.enabled)

function selectTab(next) {
  if (!tabs.includes(next)) return
  emit('update:tab', next)
}

function onTabKeydown(event) {
  const current = Math.max(0, tabs.indexOf(props.tab))
  let index = current
  if (event.key === 'ArrowRight') index = (current + 1) % tabs.length
  else if (event.key === 'ArrowLeft') index = (current - 1 + tabs.length) % tabs.length
  else if (event.key === 'Home') index = 0
  else if (event.key === 'End') index = tabs.length - 1
  else return
  event.preventDefault()
  const next = tabs[index]
  selectTab(next)
  nextTick(() => {
    document.getElementById(`provider-tab-${next}`)?.focus()
  })
}
</script>

<template>
  <div class="providers-page">
    <div class="provider-tabs" role="tablist" @keydown="onTabKeydown">
      <button id="provider-tab-ssh" type="button" class="provider-tab" role="tab" :aria-selected="tab === 'ssh'" aria-controls="provider-panel-ssh" :tabindex="tab === 'ssh' ? 0 : -1" @click="selectTab('ssh')">
        {{ $t('app.providers.ssh') }}
      </button>
      <button id="provider-tab-devtunnel" type="button" class="provider-tab" role="tab" :aria-selected="tab === 'devtunnel'" aria-controls="provider-panel-devtunnel" :tabindex="tab === 'devtunnel' ? 0 : -1" @click="selectTab('devtunnel')">
        {{ $t('app.providers.devtunnel') }}
      </button>
      <button id="provider-tab-cloudflare" type="button" class="provider-tab" role="tab" :aria-selected="tab === 'cloudflare'" aria-controls="provider-panel-cloudflare" :tabindex="tab === 'cloudflare' ? 0 : -1" @click="selectTab('cloudflare')">
        {{ $t('app.providers.cloudflare') }}
      </button>
      <button id="provider-tab-frp" type="button" class="provider-tab" role="tab" :aria-selected="tab === 'frp'" aria-controls="provider-panel-frp" :tabindex="tab === 'frp' ? 0 : -1" @click="selectTab('frp')">FRP</button>
    </div>

    <div v-if="tab === 'ssh'" id="provider-panel-ssh" role="tabpanel" aria-labelledby="provider-tab-ssh">
      <JumpersPage
        :jumpers="jumpers"
        :total-count="jumperTotal"
        :tunnels="tunnels"
        :search-query="searchQuery"
        @update-search-query="emit('update-search-query', $event)"
        @import-jumper="emit('import-jumper')"
        @new-jumper="emit('new-jumper')"
        @copy-jumper="emit('copy-jumper', $event)"
        @edit-jumper="emit('edit-jumper', $event)"
        @delete-jumper="emit('delete-jumper', $event)"
      />
    </div>
    <div v-else-if="tab === 'devtunnel'" id="provider-panel-devtunnel" role="tabpanel" aria-labelledby="provider-tab-devtunnel">
      <DevTunnelProvider
        :status="devTunnelStatus"
        :busy="devTunnelBusy"
        :resources="devTunnelResources"
        :tunnels="tunnels"
        :resource-busy="devTunnelResourceBusy"
        :created-resource="devTunnelCreated"
        :action-result="devTunnelActionResult"
        :detail-resource="devTunnelDetail"
        :create-error="devTunnelCreateError"
        :region-probe="devTunnelRegionProbe"
        :search-query="devSearchQuery"
        @update-search-query="devSearchQuery = $event"
        @reset-created="emit('reset-devtunnel-created')"
        @refresh="emit('refresh-devtunnel')"
        @refresh-resources="emit('refresh-devtunnel-resources')"
        @create-resource="emit('create-devtunnel-resource', $event)"
        @delete-resource="emit('delete-devtunnel-resource', $event)"
        @retry-cleanup="emit('retry-devtunnel-cleanup', $event)"
        @create-tunnel="emit('create-devtunnel', $event)"
        @inspect-resource="emit('inspect-devtunnel-resource', $event)"
        @open-url="emit('open-url', $event)"
        @install="emit('install-devtunnel')"
        @cancel-install="emit('cancel-devtunnel-install')"
        @login="emit('login-devtunnel', $event)"
        @cancel-login="emit('cancel-devtunnel-login')"
        @logout="emit('logout-devtunnel')"
        @open-login-url="emit('open-devtunnel-login', $event)"
        @open-docs="emit('open-devtunnel-docs')"
        @load-regions="emit('load-devtunnel-regions')"
        @test-regions="emit('test-devtunnel-regions')"
      />
    </div>
    <div v-else-if="tab === 'cloudflare'" id="provider-panel-cloudflare" role="tabpanel" aria-labelledby="provider-tab-cloudflare">
      <CloudflareProvider
        :status="cloudflareStatus"
        :busy="cloudflareBusy"
        :resources="cloudflareResources"
        :tunnels="tunnels"
        :resource-busy="cloudflareResourceBusy"
        :created-resource="cloudflareCreated"
        :action-result="cloudflareActionResult"
        :detail-resource="cloudflareDetail"
        :create-error="cloudflareCreateError"
        :zones="cloudflareZones"
        :search-query="cloudflareSearchQuery"
        @update-search-query="cloudflareSearchQuery = $event"
        @reset-created="emit('reset-cloudflare-created')"
        @refresh="emit('refresh-cloudflare')"
        @refresh-resources="emit('refresh-cloudflare-resources')"
        @refresh-zones="emit('refresh-cloudflare-zones')"
        @create-resource="emit('create-cloudflare-resource', $event)"
        @delete-resource="emit('delete-cloudflare-resource', $event)"
        @retry-cleanup="emit('retry-cloudflare-cleanup', $event)"
        @create-tunnel="emit('create-cloudflare', $event)"
        @inspect-resource="emit('inspect-cloudflare-resource', $event)"
        @install="emit('install-cloudflare')"
        @cancel-install="emit('cancel-cloudflare-install')"
        @login="emit('login-cloudflare', $event)"
        @authorizations-changed="emit('cloudflare-authorizations-changed')"
        @cancel-login="emit('cancel-cloudflare-login')"
        @open-login-url="emit('open-cloudflare-login', $event)"
        @open-docs="emit('open-cloudflare-docs')"
      />
    </div>
    <div v-else-if="tab === 'frp'" id="provider-panel-frp" role="tabpanel" aria-labelledby="provider-tab-frp">
      <button v-if="frpDraftPending" type="button" class="btn btn-sm btn-outline-primary mb-3" @click="emit('resume-frp-draft')">{{ $t('app.frp.returnDraft') }}</button>
      <FRPProvider :tunnels="tunnels" @create-tunnel="emit('create-frp-tunnel', $event)" @view-tunnels="emit('view-frp-tunnels', $event)" />
    </div>
    <section v-else class="provider-card">
      <p class="provider-state">{{ $t('app.providers.unavailable') }}</p>
    </section>
  </div>
</template>
