<script setup>
import { devTunnelServiceOwner, devTunnelNameOwner, sameTunnelID } from '../../utils/tunnel-bindings'
import { normalizeDevTunnelName, DEV_TUNNEL_NAME_PATTERN } from '../../utils/devtunnel-name'
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
const nameTouched = ref(false)

const CUSTOM_ID_SENTINEL = '__custom__'

const publicURLs = computed(() => devTunnelPortURLs(props.tunnelForm.publicUrl, props.tunnelForm.tunnelPort || props.tunnelForm.localPort))

const signedIn = computed(() => props.accountState === 'signed_in')
const resourceStatusActive = computed(() => Boolean(props.resources.state && props.resources.state !== 'ready' && props.resources.state !== 'idle'))
const showBoundSummary = computed(() => Boolean(props.editingTunnelId && props.tunnelForm.originalTunnelId))
const accessLocked = computed(() => props.tunnelForm.tunnelIdMode === 'bind' && Boolean(props.tunnelForm.tunnelId))
const regionLocked = computed(() => showBoundSummary.value || accessLocked.value)
const boundRegion = computed(() => regionLabel(props.regionProbe?.regions, devTunnelCluster(props.tunnelForm.tunnelId)) || '—')
const selectValue = computed(() => {
  if (props.tunnelForm.tunnelIdMode === 'bind') return props.tunnelForm.tunnelId || ''
  if (props.tunnelForm.tunnelIdMode === 'custom') return CUSTOM_ID_SENTINEL
  return ''
})
const selectedIDMissing = computed(() => props.tunnelForm.tunnelIdMode !== 'custom'
  && Boolean(selectValue.value)
  && !(props.resources.items || []).some((item) => item.id === selectValue.value))

const customName = computed(() => normalizeDevTunnelName(props.tunnelForm.devTunnelName))
const customNameError = computed(() => {
  if (props.tunnelForm.tunnelIdMode !== 'custom') return ''
  if (!customName.value) return nameTouched.value ? t('app.modals.tunnel.customIdRequired') : ''
  if (!DEV_TUNNEL_NAME_PATTERN.test(customName.value)) return t('app.modals.tunnel.customIdInvalid')
  return ''
})
const customNameOwner = computed(() => (customName.value
  ? devTunnelNameOwner(props.tunnels, customName.value, props.editingTunnelId)
  : null))
const customNameTakenRemotely = computed(() => Boolean(customName.value)
  && (props.resources.items || []).some((item) => sameTunnelID(item.id, customName.value)))

const serviceOwner = computed(() => devTunnelServiceOwner(props.tunnels, props.tunnelForm.tunnelIdMode === 'bind' ? props.tunnelForm.tunnelId : '', props.editingTunnelId))

function optionOwner(id) {
  return devTunnelServiceOwner(props.tunnels, id, props.editingTunnelId)
}

function optionLabel(item) {
  const owner = optionOwner(item.id)
  return owner ? t('app.modals.tunnel.occupiedIdOption', { id: item.id, name: owner.name }) : item.id
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

function onCustomNameInput(event) {
  nameTouched.value = true
  props.tunnelForm.devTunnelName = String(event?.target?.value || '').toLowerCase()
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
        <option :value="CUSTOM_ID_SENTINEL">{{ $t('app.modals.tunnel.customIdOption') }}</option>
        <option v-if="selectedIDMissing" :value="selectValue" :disabled="Boolean(optionOwner(selectValue))">{{ selectValue }}</option>
        <optgroup v-if="(resources.items || []).length" :label="$t('app.modals.tunnel.existingGroup')">
          <option
            v-for="item in resources.items"
            :key="item.id"
            :value="item.id"
            :disabled="Boolean(optionOwner(item.id))"
          >
            {{ optionLabel(item) }}
          </option>
        </optgroup>
      </select>
      <template v-if="tunnelForm.tunnelIdMode === 'custom' && !showBoundSummary">
        <input
          id="devtunnel-custom-id"
          class="form-control"
          :class="{ 'is-invalid': Boolean(customNameError) }"
          type="text"
          :value="tunnelForm.devTunnelName"
          autocapitalize="none"
          autocorrect="off"
          spellcheck="false"
          :placeholder="t('app.modals.tunnel.customIdPlaceholder')"
          @input="onCustomNameInput"
          @blur="nameTouched = true"
        />
        <div v-if="customNameError" class="field-warning">{{ customNameError }}</div>
        <div v-else-if="customNameOwner" class="field-warning">{{ t('app.modals.tunnel.customIdOccupied', { name: customNameOwner.name }) }}</div>
        <div v-else-if="customNameTakenRemotely" class="field-warning">{{ t('app.modals.tunnel.customIdRemoteConflict') }}</div>
        <div v-else class="field-note">{{ t('app.modals.tunnel.customIdHint') }}</div>
      </template>
      <div v-if="tunnelForm.resourceNote && !resourceStatusActive" class="field-note">{{ tunnelForm.resourceNote }}</div>
      <div v-if="!signedIn && !showBoundSummary && !resourceStatusActive" class="field-note">{{ $t('app.modals.tunnel.signInForIds') }}</div>
      <div v-if="resources.state === 'loading'" class="field-note">{{ $t('app.modals.tunnel.tunnelIdLoading') }}</div>
      <div v-else-if="resources.state && resources.state !== 'ready' && resources.state !== 'idle'" class="field-note">
        {{ resources.detail || $t('app.providers.loadFailed') }}
        <button type="button" class="btn btn-link btn-sm devtunnel-inline-action" @click="emit('refresh')">{{ $t('app.providers.retry') }}</button>
      </div>
      <div v-if="tunnelForm.resourceError" class="field-warning">{{ tunnelForm.resourceError }}</div>
    </div>
    <div class="col-12">
      <div class="field-note mt-0">{{ $t('app.modals.tunnel.singleIdHint') }}</div>
      <div class="field-warning">{{ $t('app.modals.tunnel.singlePortCleanup') }}</div>
      <div v-if="serviceOwner" class="field-warning">{{ $t('app.modals.tunnel.duplicateService', { name: serviceOwner.name }) }}</div>
    </div>
    <LocalServiceAddress :tunnel-form="tunnelForm" />
    <div class="col-12">
      <div class="field-note mt-0">{{ $t('app.modals.tunnel.currentPublicPort') }}：{{ tunnelForm.tunnelPort || $t('app.modals.tunnel.portAtStart') }}</div>
    </div>
    <div class="col-12">
      <label class="form-label">{{ $t('app.modals.tunnel.access') }}</label>
      <div class="devtunnel-access" role="radiogroup" :aria-label="$t('app.modals.tunnel.access')">
        <button
          type="button"
          class="devtunnel-access-option"
          role="radio"
          :aria-checked="tunnelForm.access === 'private'"
          :class="{ 'is-selected': tunnelForm.access === 'private' }"
          :disabled="accessLocked"
          @click="selectAccess('private')"
        >
          <i class="bi bi-lock" aria-hidden="true" />
          <span>{{ $t('app.modals.tunnel.accessPrivate') }}</span>
          <i v-if="tunnelForm.access === 'private'" class="bi bi-check-lg" aria-hidden="true" />
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
      <div v-if="accessLocked" class="field-note">{{ $t('app.modals.tunnel.accessReadOnly') }}</div>
      <div v-if="tunnelForm.access === 'public'" class="field-warning">{{ $t('app.modals.tunnel.publicWarning') }}</div>
      <div v-if="accessLocked && tunnelForm.access === 'unknown'" class="field-warning">{{ $t('app.modals.tunnel.access') }}：{{ $t('app.providers.accessUnknown') }}</div>
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
