<script setup>
import { devTunnelServiceOwner } from '../../utils/tunnel-bindings'
import LocalServiceAddress from './LocalServiceAddress.vue'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { devTunnelPortURLs } from '../../utils/public-urls'
import { devTunnelCluster, regionLabel } from '../../utils/devtunnel-region'
import CopyIconButton from '../common/CopyIconButton.vue'
import IconActionButton from '../common/IconActionButton.vue'
import RegionDropdown from '../common/RegionDropdown.vue'

const props = defineProps({
  tunnelForm: { type: Object, required: true },
  resources: { type: Object, default: () => ({ state: 'idle', detail: '', items: [] }) },
  tunnels: { type: Array, default: () => [] },
  editingTunnelId: { type: Number, default: null },
  accountState: { type: String, default: '' },
  regionProbe: { type: Object, default: () => ({ state: 'idle', regions: [], loading: false }) },
})

const emit = defineEmits(['manage', 'refresh', 'select-id', 'open-url', 'test-regions'])
const { t } = useI18n()
const copyError = ref('')

const publicURLs = computed(() => devTunnelPortURLs(props.tunnelForm.publicUrl, props.tunnelForm.tunnelPort || props.tunnelForm.localPort))

const signedIn = computed(() => props.accountState === 'signed_in')
const resourceStatusActive = computed(() => Boolean(props.resources.state && props.resources.state !== 'ready' && props.resources.state !== 'idle'))
const showBoundSummary = computed(() => Boolean(props.editingTunnelId && props.tunnelForm.originalTunnelId))
const accessLocked = computed(() => props.tunnelForm.tunnelIdMode === 'bind' && Boolean(props.tunnelForm.tunnelId))
const regionLocked = computed(() => showBoundSummary.value || accessLocked.value)
const boundRegion = computed(() => regionLabel(props.regionProbe?.regions, devTunnelCluster(props.tunnelForm.tunnelId)) || '—')
const selectValue = computed(() => (props.tunnelForm.tunnelIdMode === 'bind' ? props.tunnelForm.tunnelId || '' : ''))
const selectedIDMissing = computed(() => Boolean(selectValue.value) && !(props.resources.items || []).some((item) => item.id === selectValue.value))

const serviceOwner = computed(() => devTunnelServiceOwner(props.tunnels, props.tunnelForm.tunnelIdMode === 'bind' ? props.tunnelForm.tunnelId : '', props.tunnelForm.localHost, props.tunnelForm.localPort, props.editingTunnelId))

function sameDevTunnelID(left, right) {
  const a = String(left || '').trim().toLowerCase()
  const b = String(right || '').trim().toLowerCase()
  if (!a || !b) return false
  if (a === b) return true
  const [aShort, aCluster] = a.split('.')
  const [bShort, bCluster] = b.split('.')
  if (!aShort || aShort !== bShort) return false
  if (!a.includes('.') || !b.includes('.')) return true
  return aCluster === bCluster
}

function optionLabel(item) {
  const owners = props.tunnels.filter(tunnel => sameDevTunnelID(tunnel?.devTunnel?.tunnelId, item.id))
  return owners.length ? t('app.modals.tunnel.sharedIdOption', { id: item.id, count: owners.length }) : item.id
}

function selectAccess(next) {
  if (accessLocked.value) return
  props.tunnelForm.access = next
}

function onExpirationUnitChange() {
  const max = props.tunnelForm.expirationUnit === 'h' ? 720 : 30
  const amount = Number(props.tunnelForm.expirationAmount)
  if (Number.isInteger(amount) && amount > max) props.tunnelForm.expirationAmount = max
}

function onSelect(event) {
  const value = event?.target?.value || ''
  emit('select-id', value)
}

</script>

<template>
  <div class="row g-3">
    <div class="col-12">
      <div class="devtunnel-id-header">
        <label class="form-label" for="devtunnel-id">{{ $t('app.modals.tunnel.tunnelId') }}</label>
        <button type="button" class="btn btn-link btn-sm devtunnel-inline-action devtunnel-manage-action" @click="emit('manage')">
          {{ $t('app.modals.tunnel.manageIds') }}
        </button>
      </div>
      <div v-if="showBoundSummary" class="copy-value-row copy-value-row-inline">
        <span id="devtunnel-id" class="copy-value-text" :title="tunnelForm.tunnelId">{{ tunnelForm.tunnelId }}</span>
        <CopyIconButton :value="tunnelForm.tunnelId" :title="$t('app.providers.copyId')" button-class="devtunnel-detail-action" @copied="copyError = ''" @error="copyError = t('app.common.copyFailed')" />
      </div>
      <select
        v-else
        id="devtunnel-id"
        class="form-select"
        :value="selectValue"
        @change="onSelect"
      >
        <option value="">{{ $t('app.modals.tunnel.autoCreate') }}</option>
        <option v-if="selectedIDMissing" :value="selectValue">{{ selectValue }}</option>
        <optgroup v-if="(resources.items || []).length" :label="$t('app.modals.tunnel.existingGroup')">
          <option
            v-for="item in resources.items"
            :key="item.id"
            :value="item.id"
          >
            {{ optionLabel(item) }}
          </option>
        </optgroup>
      </select>
      <div v-if="tunnelForm.resourceNote && !resourceStatusActive" class="field-note">{{ tunnelForm.resourceNote }}</div>
      <div v-if="!signedIn && !showBoundSummary && !resourceStatusActive" class="field-note">{{ $t('app.modals.tunnel.signInForIds') }}</div>
      <div v-if="resources.state === 'loading'" class="field-note">{{ $t('app.modals.tunnel.tunnelIdLoading') }}</div>
      <div v-else-if="resources.state && resources.state !== 'ready' && resources.state !== 'idle'" class="field-note">
        {{ resources.detail || $t('app.providers.loadFailed') }}
        <button type="button" class="btn btn-link btn-sm devtunnel-inline-action" @click="emit('refresh')">{{ $t('app.providers.retry') }}</button>
      </div>
      <div v-if="tunnelForm.resourceError" class="field-warning">{{ tunnelForm.resourceError }}</div>
    </div>
    <LocalServiceAddress
      :tunnel-form="tunnelForm"
      :hint="serviceOwner ? $t('app.modals.tunnel.duplicateService', { name: serviceOwner.name }) : ''"
    />
    <div class="col-12">
      <label class="form-label">{{ $t('app.modals.tunnel.access') }}</label>
      <div class="devtunnel-access" role="radiogroup" :aria-label="$t('app.modals.tunnel.access')">
        <button
          type="button"
          class="devtunnel-access-option"
          role="radio"
          :aria-checked="tunnelForm.access !== 'public'"
          :class="{ 'is-selected': tunnelForm.access !== 'public' }"
          :disabled="accessLocked"
          @click="selectAccess('private')"
        >
          <i class="bi bi-lock" aria-hidden="true" />
          <span>{{ $t('app.modals.tunnel.accessPrivate') }}</span>
          <i v-if="tunnelForm.access !== 'public'" class="bi bi-check-lg" aria-hidden="true" />
        </button>
        <button
          type="button"
          class="devtunnel-access-option"
          role="radio"
          :aria-checked="tunnelForm.access === 'public'"
          :class="{ 'is-selected': tunnelForm.access === 'public' }"
          :disabled="accessLocked"
          @click="selectAccess('public')"
        >
          <i class="bi bi-globe2" aria-hidden="true" />
          <span>{{ $t('app.modals.tunnel.accessPublic') }}</span>
          <i v-if="tunnelForm.access === 'public'" class="bi bi-check-lg" aria-hidden="true" />
        </button>
      </div>
      <div v-if="tunnelForm.access === 'public'" class="field-warning">{{ $t('app.modals.tunnel.publicWarning') }}</div>
      <div v-if="accessLocked" class="field-note">{{ $t('app.modals.tunnel.accessReadOnly') }}</div>
    </div>
    <div v-if="regionLocked" class="col-12">
      <div class="field-note mb-0">{{ $t('app.modals.tunnel.regionReadOnly', { name: boundRegion }) }}</div>
    </div>
    <div v-else class="col-12">
      <RegionDropdown
        :model-value="tunnelForm.region || ''"
        :regions="regionProbe.regions || []"
        :state="regionProbe.state || 'idle'"
        :loading="!!regionProbe.loading"
        @update:model-value="tunnelForm.region = $event"
        @test="emit('test-regions')"
      />
    </div>
    <div class="col-12">
      <div class="row g-3">
        <div class="col-6">
          <label class="form-label" for="devtunnel-expiration-amount">{{ $t('app.modals.tunnel.expiration') }}</label>
          <input
            id="devtunnel-expiration-amount"
            v-model.number="tunnelForm.expirationAmount"
            class="form-control"
            type="number"
            min="1"
            :max="tunnelForm.expirationUnit === 'h' ? 720 : 30"
            step="1"
            :disabled="accessLocked"
            required
          />
        </div>
        <div class="col-6">
          <label class="form-label" for="devtunnel-expiration-unit">{{ $t('app.modals.tunnel.expirationUnit') }}</label>
          <select id="devtunnel-expiration-unit" v-model="tunnelForm.expirationUnit" class="form-select" :disabled="accessLocked" @change="onExpirationUnitChange">
            <option value="h">{{ $t('app.modals.tunnel.expirationHours') }}</option>
            <option value="d">{{ $t('app.modals.tunnel.expirationDays') }}</option>
          </select>
        </div>
      </div>
      <div v-if="tunnelForm.remoteExpiration" class="field-note">{{ $t('app.modals.tunnel.remoteExpiration', { value: tunnelForm.remoteExpiration }) }}</div>
      <div v-else class="field-note">{{ $t('app.modals.tunnel.expirationHint') }}</div>
    </div>
    <div class="col-12">
      <label class="form-label" for="devtunnel-public-url">{{ $t('app.modals.tunnel.publicUrl') }}</label>
      <div v-if="publicURLs.length" class="devtunnel-public-urls">
        <div v-for="(url, index) in publicURLs" :key="url" class="copy-value-row copy-value-row-inline">
          <span
            :id="index === 0 ? 'devtunnel-public-url' : `devtunnel-public-url-${index}`"
            class="copy-value-text"
            :title="url"
          >{{ url }}</span>
          <CopyIconButton :value="url" :title="$t('app.tunnels.actions.copyUrl')" button-class="devtunnel-detail-action" @copied="copyError = ''" @error="copyError = t('app.common.copyFailed')" />
          <IconActionButton
            button-class="devtunnel-detail-action"
            icon-class="bi-box-arrow-up-right"
            :title="$t('app.tunnels.actions.openUrl')"
            :aria-label="$t('app.tunnels.actions.openUrl')"
            @click="emit('open-url', url)"
          />
        </div>
      </div>
      <p v-else id="devtunnel-public-url" class="text-muted mb-0 tunnel-public-url-pending">{{ $t('app.modals.tunnel.publicUrlPending') }}</p>
      <div class="field-note">{{ $t('app.modals.tunnel.publicUrlHint') }}</div>
    </div>
    <p v-if="copyError" class="field-warning mb-0">{{ copyError }}</p>
  </div>
</template>
