<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ClipboardSetText, EventsOn } from '../../../wailsjs/runtime/runtime'
import * as api from '../../utils/cloud-sync-api'
import IconActionButton from './IconActionButton.vue'
import CardHeader from './CardHeader.vue'
import SecretField from './SecretField.vue'

const emit = defineEmits(['confirm-action', 'reload-state'])
const { t, locale } = useI18n()
const state = ref({ enabled: false, connected: false, state: 'disabled' })
const loaded = ref(false)
const loading = ref(true)
const loadError = ref('')
const toggling = ref(null)
const server = ref('')
const token = ref('')
const tokenValid = computed(() => /^[A-Za-z0-9_-]{43}$/.test(token.value.trim()))
const tokenInvalid = computed(() => token.value.length > 0 && !tokenValid.value)
const showToken = ref(false)
const deviceName = ref('')
const busy = ref(false)
const reconnecting = ref(false)
const message = ref('')
const messageIsError = ref(false)
const tokenFeedback = computed(() => tokenInvalid.value ? t('cloudSync.tokenInvalid') : message.value || state.value.error || t('cloudSync.tokenHint'))
const feedbackIsError = computed(() => tokenInvalid.value || (message.value ? messageIsError.value : !!state.value.error))
let unsubscribe
function updateToken(event) {
  const value = event.target.value.replace(/\s/g, '')
  event.target.value = value
  token.value = value
  message.value = ''
}
function receive(value) {
  if (!value.enabled) {
    token.value = ''
    showToken.value = false
    reconnecting.value = false
    if (state.value.enabled) { message.value = ''; messageIsError.value = false }
  }
  state.value = value
  loaded.value = true
  loading.value = false
  loadError.value = ''
  deviceName.value = value.device_name || ''
  server.value = value.server_url || ''
}
async function refresh() {
  receive(await api.GetCloudSyncStatus())
}
async function run(action) {
  if (busy.value || !loaded.value || !state.value.enabled) return
  busy.value = true
  message.value = ''
  messageIsError.value = false
  try {
    await action()
  } catch (error) {
    message.value = String(error)
    messageIsError.value = true
  } finally {
    await refresh().catch(() => {})
    busy.value = false
  }
}
function hideDraftToken() { showToken.value = false }
onMounted(async () => {
  window.addEventListener('blur', hideDraftToken)
  unsubscribe = EventsOn('cloud-sync:status', receive)
  await refresh().catch((error) => {
    loading.value = false
    loadError.value = String(error)
  })
})
onBeforeUnmount(() => {
  window.removeEventListener('blur', hideDraftToken)
  unsubscribe?.()
})
async function setEnabled(event) {
  const enabled = event.target.checked
  // The native checkbox changes before Vue renders; keep it on the last confirmed value.
  event.target.checked = !!state.value.enabled
  if (busy.value || !loaded.value) return
  busy.value = true
  toggling.value = enabled
  message.value = ''
  messageIsError.value = false
  try {
    receive(await api.SetCloudSyncEnabled(enabled))
  } catch (error) {
    message.value = String(error)
    messageIsError.value = true
  } finally {
    toggling.value = null
    busy.value = false
  }
}
function timestamp(value) {
  if (!value) return t('cloudSync.never')
  return new Date(value).toLocaleString(locale.value)
}
function connect() {
  if (!tokenValid.value) return
  return run(async () => {
    await api.ConnectCloudSync(token.value.trim())
    reconnecting.value = false
    token.value = ''
    showToken.value = false
  })
}
function test() {
  if (!tokenValid.value) return
  return run(async () => {
    await api.TestCloudConnection(token.value.trim())
    message.value = t('cloudSync.testSuccess')
  })
}
function generate() {
  return run(async () => {
    const generated = await api.GenerateCloudSyncToken()
    token.value = generated
    showToken.value = true
    message.value = t('cloudSync.generated')
  })
}
function preferences(automatic = state.value.automatic) {
  return run(() => api.SetCloudSyncPreferences(automatic, deviceName.value.trim()))
}
function resolve(choice) {
  // Capture the exact confirmation target now, not when the dialog callback runs.
  const revision = state.value.revision
  const fingerprint = state.value.local_fingerprint
  emit('confirm-action', {
    mode: 'confirm',
    message: t(choice === 'cloud' ? 'cloudSync.confirmCloud' : 'cloudSync.confirmLocal', { revision }),
    confirmLabel: t(choice === 'cloud' ? 'cloudSync.useCloud' : 'cloudSync.keepLocal'),
    onConfirm: () =>
      run(async () => {
        await api.ResolveCloudSync(choice, revision, fingerprint)
        emit('reload-state')
      }),
  })
}
function disconnect() {
  emit('confirm-action', {
    mode: 'confirm',
    message: t('cloudSync.confirmDisconnect'),
    onConfirm: () => run(() => api.DisconnectCloudSync()),
  })
}
function cancelReconnect() {
  reconnecting.value = false
  token.value = ''
  showToken.value = false
}
async function copyToken() {
  if (busy.value || !loaded.value || !state.value.enabled) return
  messageIsError.value = false
  try {
    if (!state.value.connected || reconnecting.value) {
      if (!token.value.trim()) return
      if (!(await ClipboardSetText(token.value.trim()))) throw new Error(t('cloudSync.copyFailed'))
    } else {
      await api.CopyCloudSyncToken()
    }
    if (state.value.connected && !reconnecting.value) {
      message.value = ''
    } else {
      message.value = t('cloudSync.copied')
    }
    return true
  } catch (error) {
    message.value = String(error)
    messageIsError.value = true
    return false
  }
}
</script>

<template>
  <div class="cloud-sync-card small">
    <div class="lt-card">
    <CardHeader :title="t('cloudSync.title')" icon="bi-cloud-check">
      <template #status>
        <span v-if="loaded && !reconnecting" class="sync-status lt-pill" :class="state.state === 'synced' ? 'lt-pill--success' : ['error', 'conflict', 'disconnected'].includes(state.state) ? 'lt-pill--danger' : ['idle', 'pending', 'update_available'].includes(state.state) ? 'lt-pill--warning' : 'lt-pill--neutral'" role="status">
          <span class="sync-status-dot" aria-hidden="true"></span>{{ t(`cloudSync.states.${state.state}`) }}
        </span>
      </template>
      <div class="sync-header-actions">
        <button v-if="loaded && state.enabled && state.connected && !reconnecting" class="btn btn-sm btn-primary sync-now" :disabled="busy || state.state === 'syncing'" @click="run(() => api.CloudSyncNow())">{{ t('cloudSync.syncNow') }}</button>
        <span v-if="toggling !== null" class="text-body-secondary" role="status">{{ t(toggling ? 'cloudSync.enabling' : 'cloudSync.disabling') }}</span>
        <div class="sync-enable-control">
          <label for="sync-enabled" class="mb-0">{{ t('cloudSync.enabled') }}</label>
          <div class="form-check form-switch m-0">
            <input id="sync-enabled" type="checkbox" class="form-check-input" :checked="state.enabled" :disabled="!loaded || busy" aria-describedby="sync-enabled-hint" @change="setEnabled" />
          </div>
        </div>
      </div>
    </CardHeader>
    <div class="lt-card-body">
    <p v-if="loading" class="text-body-secondary mb-0" role="status">{{ t('cloudSync.loading') }}</p>
    <p v-else-if="loadError" class="is-error text-break mb-0" role="alert">{{ loadError }}</p>
    <template v-else-if="!state.enabled">
      <p class="mb-2">{{ t('cloudSync.disabledDescription') }}</p>
      <p id="sync-enabled-hint" class="text-body-secondary mb-0">{{ t('cloudSync.disabledHint') }}</p>
    </template>
    <template v-else>
    <small id="sync-enabled-hint" class="visually-hidden">{{ t('cloudSync.disabledHint') }}</small>
    <template v-if="!state.connected || reconnecting">
      <div class="sync-device mb-3">
        <span class="form-label mb-0">{{ t('cloudSync.deviceName') }}</span>
        <span class="text-break fw-medium">{{ deviceName || '—' }}</span>
      </div>
      <div class="d-flex align-items-center gap-1 mb-1">
        <label class="form-label mb-0" for="sync-token">{{ t('cloudSync.token') }}</label>
        <IconActionButton
          button-class="btn-link sync-token-help"
          icon-class="bi-question-circle"
          :title="t('cloudSync.tokenHint')"
          :aria-label="t('cloudSync.tokenHint')"
        />
      </div>
      <div class="sync-token-field mb-3">
      <div class="sync-token-row">
        <div class="sync-token-input">
        <input
          id="sync-token"
          :value="token"
          :type="showToken ? 'text' : 'password'"
          autocomplete="off"
          pattern="[A-Za-z0-9_-]{43}"
          spellcheck="false"
          class="form-control form-control-sm"
          :class="{ 'is-invalid': tokenInvalid }"
          :aria-invalid="tokenInvalid"
          :aria-describedby="tokenFeedback ? 'sync-token-feedback' : undefined"
          :disabled="busy"
          :title="t('cloudSync.copyToken')"
          @click="copyToken"
          @keydown.alt.c.prevent="copyToken"
          @input="updateToken"
        />
        <IconActionButton
          button-class="btn-link sync-token-visibility"
          :icon-class="showToken ? 'bi-eye-slash' : 'bi-eye'"
          :title="t(showToken ? 'cloudSync.hideToken' : 'cloudSync.showToken')"
          :aria-label="t(showToken ? 'cloudSync.hideToken' : 'cloudSync.showToken')"
          :aria-pressed="showToken"
          :disabled="busy || !token"
          @click.stop="showToken = !showToken"
        />
        </div>
        <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="busy || !server || !deviceName" @click="generate">
          {{ t('cloudSync.generate') }}
        </button>
      </div>
      <div v-if="tokenFeedback" id="sync-token-feedback" class="sync-token-feedback text-break" :class="{ 'is-error': feedbackIsError }" :role="feedbackIsError ? 'alert' : 'status'">
        {{ tokenFeedback }}
      </div>
      </div>
      <div class="d-flex flex-wrap gap-2">
        <button class="btn btn-sm btn-outline-secondary" :disabled="busy || !server || !tokenValid" @click="test">
          {{ t('cloudSync.test') }}
        </button>
        <button class="btn btn-sm btn-primary" :disabled="busy || !server || !tokenValid || !deviceName.trim()" @click="connect">
          {{ t('cloudSync.connect') }}
        </button>
        <button v-if="reconnecting" class="btn btn-sm btn-outline-secondary" :disabled="busy" @click="cancelReconnect">
          {{ t('cloudSync.cancel') }}
        </button>
      </div>
      <p class="text-body-secondary mt-2 mb-0">{{ t('cloudSync.connectHint') }}</p>
    </template>
    <template v-else>
      <dl class="lt-kv sync-summary">
        <dt class="text-body-secondary">{{ t('cloudSync.lastSynced') }}</dt>
        <dd class="fw-medium">
          {{ timestamp(state.last_synced_at) }}
          <small class="sync-secondary text-body-secondary">{{ t('cloudSync.localVersion', { revision: state.last_synced_revision }) }}</small>
        </dd>
        <dt class="text-body-secondary">{{ t('cloudSync.cloudVersion') }}</dt>
        <dd class="fw-medium">Revision {{ state.revision }}</dd>
        <dt class="text-body-secondary">{{ t('cloudSync.lastUpdate') }}</dt>
        <dd class="fw-medium">
          {{ timestamp(state.update_time) }}
          <small class="sync-secondary text-body-secondary text-break">{{ state.updated_by || '—' }}</small>
        </dd>
        <dt class="text-body-secondary">{{ t('cloudSync.automatic') }}</dt>
        <dd>
          <div class="form-check form-switch m-0">
            <input id="sync-auto" type="checkbox" class="form-check-input" :checked="state.automatic" :disabled="busy" :aria-label="t('cloudSync.automatic')" aria-describedby="sync-auto-hint" @change="preferences($event.target.checked)" />
          </div>
          <small id="sync-auto-hint" class="sync-secondary text-body-secondary">{{ t('cloudSync.automaticHint') }}</small>
        </dd>
      </dl>
      <div v-if="['update_available', 'conflict'].includes(state.state)" class="alert" :class="state.state === 'conflict' ? 'alert-danger' : 'alert-warning'">
        <p>
          {{
            t(state.state === 'conflict' ? 'cloudSync.conflictHint' : 'cloudSync.updateHint', {
              revision: state.revision,
            })
          }}
        </p>
        <div class="d-flex gap-2">
          <button class="btn btn-sm btn-primary" :disabled="busy" @click="resolve('cloud')">
            {{ t('cloudSync.useCloud') }}
          </button>
          <button
            v-if="state.state === 'conflict'"
            class="btn btn-sm btn-outline-secondary"
            :disabled="busy"
            @click="resolve('local')"
          >
            {{ t('cloudSync.keepLocal') }}
          </button>
        </div>
      </div>
    </template>
    <p class="config-desc mt-3 mb-0">{{ t('cloudSync.retentionHint') }}</p>
    </template>
    </div>
    </div>
    <template v-if="loaded && state.enabled && state.connected && !reconnecting">
      <div class="lt-card">
        <CardHeader :title="t('cloudSync.connection')" icon="bi-pc-display" />
        <div class="lt-card-body">
        <dl class="lt-kv sync-connection">
          <dt class="text-body-secondary">{{ t('cloudSync.deviceName') }}</dt>
          <dd class="text-break fw-medium">{{ deviceName || '—' }}</dd>
          <dt class="text-body-secondary">{{ t('cloudSync.server') }}</dt>
          <dd class="fw-medium">
            <div class="d-flex align-items-center flex-wrap gap-2">
              <span class="lt-pill lt-pill--success">{{ t('cloudSync.connected') }}</span>
            </div>
          </dd>
          <dt class="text-body-secondary">{{ t('cloudSync.token') }}</dt>
          <dd>
            <SecretField :revealable="false" :disabled="busy" :copy-action="copyToken" />
            <div class="sync-manage-actions">
              <button type="button" class="btn btn-sm btn-outline-primary" :disabled="busy" @click="reconnecting = true">{{ t('cloudSync.reconnect') }}</button>
              <button type="button" class="btn btn-sm btn-link text-danger text-decoration-none" :disabled="busy" @click="disconnect">{{ t('cloudSync.disconnect') }}</button>
            </div>
          </dd>
        </dl>
        </div>
      </div>
    </template>
    <div v-if="loaded && ((!state.enabled || (state.connected && !reconnecting)) && (state.error || message))" class="config-desc mt-2 text-break" :class="{ 'is-error': feedbackIsError }" :role="feedbackIsError ? 'alert' : 'status'">{{ message || state.error }}</div>
  </div>
</template>

<style scoped>
.cloud-sync-card .form-label { color: var(--bs-secondary-color); }
.cloud-sync-card .form-control { min-height: 32px; }
.cloud-sync-card { display: grid; gap: 16px; }
.sync-header-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 12px; min-width: 0; font-size: 13px; line-height: 20px; }
.sync-enable-control { display: flex; align-items: center; gap: 8px; }
.sync-enable-control .form-switch { flex-shrink: 0; }
.sync-now { --bs-btn-padding-y: 1px; --bs-btn-padding-x: 8px; --bs-btn-font-size: 13px; --bs-btn-line-height: 20px; white-space: nowrap; }
.sync-status-dot { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.sync-secondary { margin-top: 4px; }
.sync-manage-actions { display: flex; align-items: center; gap: 0.75rem; flex-wrap: wrap; margin-top: 1rem; }
.sync-manage-actions .btn { font-weight: 400; }

.cloud-sync-card .alert { margin-top: 1rem; margin-bottom: 0; }
.cloud-sync-card .alert p { margin-bottom: 0.5rem; }
.cloud-sync-card .alert .d-flex { flex-wrap: wrap; }
.sync-token-row { display: flex; gap: 0.5rem; }
.sync-token-feedback { margin-top: 0.25rem; font-size: 0.78rem; line-height: 1.4; height: 2.8em; overflow-y: auto; letter-spacing: normal; color: var(--bs-secondary-color); }
.cloud-sync-card .is-error { color: var(--lt-danger-ink); }
.sync-token-input { position: relative; flex: 1; min-width: 0; }
.sync-token-input .form-control { padding-right: 2.75rem; }
.sync-token-input .form-control.is-invalid { background-image: none; }
.sync-token-input .form-control::-ms-reveal { display: none; }
.sync-token-visibility { position: absolute; right: 0.25rem; top: 50%; transform: translateY(-50%); border: 0; color: var(--bs-secondary-color); text-decoration: none; }
.sync-token-visibility:hover { color: var(--lt-ink); }
.sync-token-row .btn { white-space: nowrap; }
.sync-device { display: flex; flex-direction: column; gap: 0.25rem; }
.sync-token-help { width: 24px; min-width: 24px; height: 24px; padding: 0.15rem; color: var(--bs-secondary-color); }
@media (max-width: 480px) {
  .sync-summary { grid-template-columns: minmax(0, 1fr); row-gap: 0.25rem; }
  .sync-summary dd + dt { margin-top: 0.5rem; }
  .sync-token-row { flex-wrap: wrap; }
  .sync-token-input { flex-basis: 100%; }
}
</style>
