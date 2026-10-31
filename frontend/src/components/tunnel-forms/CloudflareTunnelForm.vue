<script setup>
import { cloudflareHostnameOwner } from '../../utils/tunnel-bindings'
import LocalServiceAddress from './LocalServiceAddress.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import CopyIconButton from '../common/CopyIconButton.vue'
import IconActionButton from '../common/IconActionButton.vue'
import { cloudflareOptionDisabled, cloudflarePublicURL, joinHostname, normalizeHostname, shortTunnelID, splitHostname } from '../../utils/cloudflare'

const props = defineProps({
  tunnelForm: { type: Object, required: true },
  resources: { type: Object, default: () => ({ state: 'idle', detail: '', items: [] }) },
  tunnels: { type: Array, default: () => [] },
  editingTunnelId: { type: Number, default: null },
  accountState: { type: String, default: '' },
  zones: { type: Object, default: () => ({ state: 'idle', detail: '', items: [] }) },
})

const emit = defineEmits(['manage', 'refresh', 'select-id', 'open-url', 'refresh-zones'])
const { t } = useI18n()
const copyError = ref('')

const signedIn = computed(() => props.accountState === 'signed_in')
const resourceStatusActive = computed(() => Boolean(props.resources.state && props.resources.state !== 'ready' && props.resources.state !== 'idle'))
const showBoundSummary = computed(() => Boolean(props.editingTunnelId && props.tunnelForm.originalTunnelId))
const selectValue = computed(() => (props.tunnelForm.tunnelIdMode === 'bind' ? props.tunnelForm.tunnelId || '' : ''))
const hostnameOwner = computed(() => cloudflareHostnameOwner(props.tunnels, props.tunnelForm.hostname, props.editingTunnelId))
const publicURL = computed(() => cloudflarePublicURL(props.tunnelForm.hostname))
const zoneItems = computed(() => (props.zones?.items || []).filter((zone) => (!zone.state || zone.state === 'ready') && normalizeHostname(zone?.name)))
const zoneNames = computed(() => zoneItems.value.map((zone) => normalizeHostname(zone.name)))
const hostnameMatchesZone = computed(() => {
  const host = normalizeHostname(props.tunnelForm.hostname)
  if (!host) return true
  return zoneNames.value.some((name) => host === name || host.endsWith(`.${name}`))
})
const useZoneSelect = computed(() => zoneNames.value.length > 0 && hostnameMatchesZone.value)
const hostLabel = computed({
  get() { return splitHostname(props.tunnelForm.hostname, zoneItems.value).label },
  set(value) { props.tunnelForm.hostname = joinHostname(value, splitHostname(props.tunnelForm.hostname, zoneItems.value).zone || zoneNames.value[0]) },
})
function onZoneChange(event) {
  const zone = event?.target?.value || zoneNames.value[0] || ''
  props.tunnelForm.hostname = joinHostname(hostLabel.value, zone)
}
watch(zoneNames, (names) => {
  if (!names.length || normalizeHostname(props.tunnelForm.hostname)) return
  props.tunnelForm.hostname = names[0]
}, { immediate: true })
const selectedIDMissing = computed(() => Boolean(selectValue.value) && !(props.resources.items || []).some((item) => item.id === selectValue.value))

function ownerName(id) {
  const owner = props.tunnels.find((tunnel) => {
    if (props.editingTunnelId && Number(tunnel.id) === Number(props.editingTunnelId)) return false
    return (tunnel.provider || 'ssh') === 'cloudflare' && String(tunnel?.cloudflare?.tunnelId || '').toLowerCase() === String(id || '').toLowerCase()
  })
  return owner?.name || ''
}

function optionLabel(item) {
  const owner = ownerName(item.id)
  const id = shortTunnelID(item.id)
  if (owner) return t('app.cloudflare.usedBy', { name: item.name || id, owner })
  if (item.credentialReady === false) return t('app.cloudflare.optionMissing', { name: item.name || id, id })
  return t('app.cloudflare.optionReady', { name: item.name || id, id })
}

function onSelect(event) {
  emit('select-id', event?.target?.value || '')
}
</script>

<template>
  <div class="row g-3">
    <div class="col-12">
      <div class="devtunnel-id-header">
        <label class="form-label" for="cloudflare-id">{{ $t('app.cloudflare.tunnelId') }}</label>
        <button type="button" class="btn btn-link btn-sm devtunnel-inline-action devtunnel-manage-action" @click="emit('manage')">
          {{ $t('app.cloudflare.manageIds') }}
        </button>
      </div>
      <div v-if="showBoundSummary" class="copy-value-row copy-value-row-inline">
        <span id="cloudflare-id" class="copy-value-text" :title="tunnelForm.tunnelId">{{ tunnelForm.tunnelId }}</span>
        <CopyIconButton :value="tunnelForm.tunnelId" :title="$t('app.cloudflare.copyId')" button-class="devtunnel-detail-action" @copied="copyError = ''" @error="copyError = t('app.common.copyFailed')" />
      </div>
      <select v-else id="cloudflare-id" class="form-select" :value="selectValue" @change="onSelect">
        <option value="">{{ $t('app.cloudflare.autoCreate') }}</option>
        <option v-if="selectedIDMissing" :value="selectValue">{{ selectValue }}</option>
        <optgroup v-if="(resources.items || []).length" :label="$t('app.cloudflare.existingGroup')">
          <option
            v-for="item in resources.items"
            :key="item.id"
            :value="item.id"
            :disabled="cloudflareOptionDisabled(item, ownerName(item.id))"
          >
            {{ optionLabel(item) }}
          </option>
        </optgroup>
      </select>
      <div v-if="tunnelForm.resourceNote && !resourceStatusActive" class="field-note">{{ tunnelForm.resourceNote }}</div>
      <div v-if="!signedIn && !showBoundSummary && !resourceStatusActive" class="field-note">{{ $t('app.cloudflare.signInForIds') }}</div>
      <div v-if="resources.state === 'loading'" class="field-note">{{ $t('app.modals.tunnel.tunnelIdLoading') }}</div>
      <div v-else-if="resources.state && resources.state !== 'ready' && resources.state !== 'idle'" class="field-note">
        {{ resources.detail || $t('app.providers.loadFailed') }}
        <button type="button" class="btn btn-link btn-sm devtunnel-inline-action" @click="emit('refresh')">{{ $t('app.providers.retry') }}</button>
      </div>
      <div v-if="tunnelForm.resourceError" class="field-warning">{{ tunnelForm.resourceError }}</div>
    </div>
    <div v-if="tunnelForm.tunnelIdMode !== 'bind'" class="col-12">
      <label class="form-label" for="cloudflare-name">{{ $t('app.cloudflare.tunnelName') }}</label>
      <input id="cloudflare-name" v-model="tunnelForm.tunnelName" class="form-control" type="text" autocapitalize="none" autocorrect="off" spellcheck="false" />
    </div>
    <LocalServiceAddress :tunnel-form="tunnelForm" />
    <div v-if="useZoneSelect" class="col-md-5">
      <label class="form-label" for="cloudflare-hostname">{{ $t('app.cloudflare.hostLabel') }}</label>
      <input id="cloudflare-hostname" v-model="hostLabel" class="form-control" type="text" autocapitalize="none" autocorrect="off" spellcheck="false" placeholder="dev" />
    </div>
    <div v-if="useZoneSelect" class="col-md-7">
      <label class="form-label" for="cloudflare-zone">{{ $t('app.cloudflare.authorizedZone') }}</label>
      <select id="cloudflare-zone" class="form-select" :value="splitHostname(tunnelForm.hostname, zoneItems).zone" @change="onZoneChange">
        <option v-for="name in zoneNames" :key="name" :value="name">{{ name }}</option>
      </select>
    </div>
    <div v-if="useZoneSelect" class="col-12">
      <div class="field-note">{{ $t('app.cloudflare.zoneHint') }}</div>
    </div>
    <div v-else class="col-12">
      <label class="form-label" for="cloudflare-hostname">{{ $t('app.cloudflare.hostname') }}</label>
      <input id="cloudflare-hostname" v-model="tunnelForm.hostname" class="form-control" type="text" autocapitalize="none" autocorrect="off" spellcheck="false" placeholder="devspace.example.com" />
      <div v-if="zones.state === 'loading'" class="field-note">{{ $t('app.cloudflare.zoneLoading') }}</div>
      <div v-else-if="signedIn && zones.state && zones.state !== 'ready' && zones.state !== 'idle'" class="field-note">
        {{ $t('app.cloudflare.zoneFailed') }}
        <button type="button" class="btn btn-link btn-sm devtunnel-inline-action" @click="emit('refresh-zones')">{{ $t('app.providers.retry') }}</button>
      </div>
      <div v-else-if="zoneNames.length" class="field-note">{{ $t('app.cloudflare.authorizedZones', { names: zoneNames.join(', ') }) }}</div>
    </div>
    <p v-if="hostnameOwner" class="field-warning mb-0">{{ $t('app.cloudflare.duplicateHostname', { name: hostnameOwner.name }) }}</p>
    <div class="col-12">
      <label class="form-label" for="cloudflare-public-url">{{ $t('app.modals.tunnel.publicUrl') }}</label>
      <div v-if="publicURL" class="copy-value-row copy-value-row-inline">
        <span id="cloudflare-public-url" class="copy-value-text" :title="publicURL">{{ publicURL }}</span>
        <CopyIconButton :value="publicURL" :title="$t('app.tunnels.actions.copyUrl')" button-class="devtunnel-detail-action" @copied="copyError = ''" @error="copyError = t('app.common.copyFailed')" />
        <IconActionButton icon-class="bi bi-box-arrow-up-right" :title="$t('app.tunnels.actions.openUrl')" :aria-label="$t('app.tunnels.actions.openUrl')" button-class="devtunnel-detail-action" @click="emit('open-url', publicURL)" />
      </div>
      <p v-else id="cloudflare-public-url" class="text-muted mb-0 tunnel-public-url-pending">{{ $t('app.cloudflare.publicUrlPending') }}</p>
    </div>
    <p v-if="copyError" class="field-warning mb-0">{{ copyError }}</p>
  </div>
</template>
