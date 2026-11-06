<script setup>
import CLIUpdateDialog from '../common/CLIUpdateDialog.vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TooltipText from '../common/TooltipText.vue'
import IconActionButton from '../common/IconActionButton.vue'
import CopyIconButton from '../common/CopyIconButton.vue'
import ProviderResourceToolbar from '../common/ProviderResourceToolbar.vue'
import ProviderRowActions from '../common/ProviderRowActions.vue'
import RegionDropdown from '../common/RegionDropdown.vue'
import { uniquePublicURLs } from '../../utils/public-urls'
import { devTunnelCluster, regionLabel } from '../../utils/devtunnel-region'
import { isValidDevTunnelName, normalizeDevTunnelName } from '../../utils/devtunnel-name'
import { devTunnelNameOwner, sameTunnelID } from '../../utils/tunnel-bindings'
import { writeTextToClipboard } from '../../utils/clipboard'
import { PreviewProviderResourceDeletion } from '../../../wailsjs/go/main/App'

const props = defineProps({
  status: { type: Object, default: () => ({}) },
  busy: { type: String, default: '' },
  resources: { type: Object, default: () => ({ state: 'idle', detail: '', items: [] }) },
  tunnels: { type: Array, default: () => [] },
  resourceBusy: { type: String, default: '' },
  createdResource: { type: Object, default: null },
  actionResult: { type: Object, default: null },
  detailResource: { type: Object, default: null },
  createError: { type: String, default: '' },
  searchQuery: { type: String, default: '' },
  regionProbe: { type: Object, default: () => ({ state: 'idle', regions: [], loading: false }) },
})

const emit = defineEmits([
  'refresh',
  'install',
  'cancel-install',
  'login',
  'cancel-login',
  'logout',
  'open-login-url',
  'open-docs',
  'refresh-resources',
  'create-resource',
  'delete-resource',
  'retry-cleanup',
  'create-tunnel',
  'inspect-resource',
  'open-url',
  'reset-created',
  'update-search-query',
  'load-regions',
  'test-regions',
])
const { t } = useI18n()
const cliMaintenanceOpen = ref(false)

const accountState = computed(() => props.status?.accountState || 'unknown')
const loginKind = computed(() => (String(props.busy).startsWith('login:') ? String(props.busy).slice(6) : ''))
const loginBusy = computed(() => accountState.value !== 'signed_in' && (props.status?.loginInProgress || (loginKind.value !== '' && loginKind.value !== 'cancel')))
const installLabel = computed(() => t(props.status?.installed ? 'app.providers.finishingInstall' : 'app.providers.installing'))
const initialChecking = computed(() => props.status?.checking && typeof props.status?.installed !== 'boolean')
const cliStatusText = computed(() => {
  if (props.busy === 'install') return installLabel.value
  if (initialChecking.value) return t('app.providers.checking')
  if (props.status?.installed === true) return t('app.providers.installed')
  if (props.status?.installed === false) return t('app.providers.notInstalled')
  return t('app.providers.unknown')
})
const cliValue = computed(() => (
  props.busy === 'install' || props.status?.installed !== true ? '' : (props.status?.version || '')
))
const phaseNote = computed(() => {
  if (props.busy === 'logout') return t('app.providers.signingOut')
  if (!loginBusy.value) return ''
  if (props.status?.loginPhase === 'verifying') return t('app.providers.verifyingLogin')
  if (props.status?.loginPhase === 'browser') return t('app.providers.waitingLogin')
  return t('app.providers.startingLogin')
})
const controlsLocked = computed(() => {
  const value = String(props.busy || '')
  return props.status?.checking || (value !== '' && value !== 'checked')
})
const checkLabel = computed(() => {
  if (props.busy === 'check' || initialChecking.value) return t('app.providers.checking')
  if (props.busy === 'checked') return t('app.providers.checked')
  return t('app.providers.check')
})
const accountBadge = computed(() => {
  if (initialChecking.value) return t('app.providers.checking')
  if (props.busy === 'logout') return t('app.providers.signingOut')
  if (loginBusy.value) return t('app.providers.signingIn')
  if (accountState.value === 'signed_in') return t('app.providers.signedIn')
  if (accountState.value === 'expired') return t('app.providers.expired')
  if (accountState.value === 'signed_out') return t('app.providers.signedOut')
  return t('app.providers.unknown')
})
const accountValue = computed(() => {
  if (!props.status?.installed || loginBusy.value || props.busy === 'logout') return ''
  if (accountState.value === 'signed_in') return props.status?.accountLabel || ''
  return ''
})
const accountMenuItems = computed(() => [{
  key: 'logout',
  label: props.busy === 'logout' ? t('app.providers.signingOut') : t('app.providers.signOut'),
  disabled: controlsLocked.value,
}])
const items = computed(() => props.resources?.items || [])
const resourceState = computed(() => props.resources?.state || 'idle')
const signedIn = computed(() => accountState.value === 'signed_in')
const visibleItems = computed(() => {
  const query = String(props.searchQuery || '').trim().toLowerCase()
  if (!query) return items.value
  return items.value.filter((item) => {
    const id = String(item?.id || '').toLowerCase()
    return id.includes(query) || regionText(item?.id).toLowerCase().includes(query)
  })
})
const totalCount = computed(() => {
  if (!signedIn.value) return null
  if (resourceState.value === 'ready' || items.value.length) return items.value.length
  return null
})
const showReading = computed(() => signedIn.value && !items.value.length && (resourceState.value === 'loading' || resourceState.value === 'idle'))
const showResourceError = computed(() => signedIn.value && !['ready', 'idle', 'loading'].includes(resourceState.value))
const showEmpty = computed(() => signedIn.value && resourceState.value === 'ready' && !items.value.length)
const showNoMatch = computed(() => signedIn.value && items.value.length > 0 && !visibleItems.value.length)
const canCreate = computed(() => signedIn.value && props.status?.installed === true && props.resourceBusy !== 'create')

const createOpen = ref(false)
const createAccess = ref('private')
const createAmount = ref(30)
const createUnit = ref('d')
const createRegion = ref('')
const createError = ref('')
const createName = ref('')
const createNameValue = computed(() => normalizeDevTunnelName(createName.value))
const createNameOwner = computed(() => devTunnelNameOwner(props.tunnels, createNameValue.value, null))
const createNameRemoteConflict = computed(() => Boolean(createNameValue.value)
  && (props.resources?.items || []).some(item => sameTunnelID(item.id, createNameValue.value)))
const createNameError = computed(() => {
  if (!createNameValue.value) return ''
  if (!isValidDevTunnelName(createNameValue.value)) return t('app.modals.tunnel.customIdInvalid')
  if (createNameOwner.value) return t('app.modals.tunnel.customIdOccupied', { name: createNameOwner.value.name })
  if (createNameRemoteConflict.value) return t('app.modals.tunnel.customIdRemoteConflict')
  return ''
})
const deleteTarget = ref(null)
const deleteError = ref('')
const deletePreview = ref(null)
const deleteRetryId = ref('')
const detailOpen = ref(false)
const copyError = ref('')
const detailCopyTarget = ref('')
const detailPublicURLs = computed(() => uniquePublicURLs(props.detailResource?.resource?.urls || []))
const copiedId = ref('')
let copyFeedbackTimer = null
onBeforeUnmount(() => window.clearTimeout(copyFeedbackTimer))

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

function linkedTunnels(id) {
  return props.tunnels.filter((tunnel) => (tunnel.provider || 'ssh') === 'devtunnel' && sameDevTunnelID(tunnel?.devTunnel?.tunnelId, id))
}

function linkText(count) {
  return count ? t('app.providers.resources.linkedCount', { count }) : t('app.providers.resources.unlinked')
}

function portLabel(item) {
  const ports = item?.ports || []
  if (ports.length === 0) return t(item?.portsError ? 'app.providers.portsFailed' : item?.portsKnown ? 'app.providers.portsEmpty' : 'app.providers.portsUnknown')
  if (ports.length === 1) return String(ports[0].port)
  return t('app.providers.portCount', { count: ports.length })
}

function portNumbers(item) {
  return (item?.ports || []).map((port) => String(port?.port ?? '')).filter(Boolean)
}

function portText(item) {
  if (item?.portsError) return t('app.providers.portsFailed')
  const labels = portNumbers(item)
  if (!labels.length) return t(item?.portsKnown ? 'app.providers.portsEmpty' : 'app.providers.portsUnknown')
  if (labels.length <= 2) return labels.join(', ')
  return `${labels[0]}, ${labels[1]} +${labels.length - 2}`
}

function portTitle(item) {
  if (item?.portsError) return String(item.portsError)
  const labels = portNumbers(item)
  return labels.length ? labels.join(', ') : portText(item)
}

function regionText(id) {
  return regionLabel(props.regionProbe?.regions, devTunnelCluster(id)) || '—'
}

function openCreate() {
  createAccess.value = 'private'
  createAmount.value = 30
  createUnit.value = 'd'
  createRegion.value = ''
  createError.value = ''
  createName.value = ''
  createOpen.value = true
  emit('reset-created')
  emit('load-regions')
}

function submitCreate() {
  createError.value = ''
  const name = createNameValue.value
  if (name && !isValidDevTunnelName(name)) {
    createError.value = t('app.modals.tunnel.customIdInvalid')
    return
  }
  if (createNameOwner.value) {
    createError.value = t('app.modals.tunnel.customIdOccupied', { name: createNameOwner.value.name })
    return
  }
  if (createNameRemoteConflict.value) {
    createError.value = t('app.modals.tunnel.customIdRemoteConflict')
    return
  }
  emit('create-resource', {
    access: createAccess.value,
    expiration: `${createAmount.value}${createUnit.value}`,
    region: createRegion.value || '',
    tunnelName: name,
  })
}

onMounted(() => emit('load-regions'))

watch(() => props.createdResource, (resource) => {
  if (resource?.id && createOpen.value) createError.value = ''
})

watch(() => props.actionResult, (result) => {
  if (!deleteTarget.value || !result) return
  if (result.state === 'deleted') {
    deleteTarget.value = null
    deleteError.value = ''
    deletePreview.value = null
    deleteRetryId.value = ''
    return
  }
  deleteRetryId.value = result.operationId || ''
  if (result.state === 'linked') {
    deleteError.value = t('app.providers.deleteLinked', { name: result.tunnelName || '' })
    return
  }
  if (result.state === 'running') {
    deleteError.value = t('app.providers.deleteRunning', { name: result.tunnelName || '' })
    return
  }
  deleteError.value = result.detail || t('app.providers.loadFailed')
})

function openDetail(item) {
  copyError.value = ''
  detailCopyTarget.value = ''
  detailOpen.value = true
  emit('inspect-resource', item.id)
}

async function copyId(id) {
  const value = String(id || '')
  if (!value) return
  if (!(await writeTextToClipboard(value))) return
  copiedId.value = value
  window.clearTimeout(copyFeedbackTimer)
  copyFeedbackTimer = window.setTimeout(() => {
    copiedId.value = ''
  }, 1500)
}

function accessLabel(value) {
  if (value === 'public') return t('app.modals.tunnel.accessPublic')
  if (value === 'private') return t('app.modals.tunnel.accessPrivate')
  return t('app.providers.accessUnknown')
}

function resourceMenuItems(item) {
  return [
    { key: 'details', label: t('app.providers.resourceActions.details') },
    { key: 'copy', label: copiedId.value === item.id ? t('app.providers.copiedId') : t('app.providers.resourceActions.copyId') },
    { key: 'create', label: t('app.providers.resourceActions.useId') },
    { key: 'delete', label: t('app.providers.resourceActions.deleteId'), danger: true, separatorBefore: true },
  ]
}

function onResourceAction(key, item) {
  if (key === 'details') openDetail(item)
  else if (key === 'copy') copyId(item.id)
  else if (key === 'create') emit('create-tunnel', item.id)
  else if (key === 'delete') {
    deleteTarget.value = item
    deleteError.value = ''
    deletePreview.value = null
    deleteRetryId.value = ''
    PreviewProviderResourceDeletion('devtunnel', item.id).then((preview) => {
      if (deleteTarget.value?.id === item.id) deletePreview.value = preview
    }).catch((err) => {
      if (deleteTarget.value?.id === item.id) deleteError.value = err?.message || String(err)
    })
  }
}

function onAccountAction(key) {
  if (key === 'logout') emit('logout')
}
</script>

<template>
  <CLIUpdateDialog v-if="cliMaintenanceOpen" provider="devtunnel" @close="cliMaintenanceOpen = false" @changed="emit('refresh')" />
  <div class="provider-stack">
    <section class="provider-summary" :aria-busy="loginBusy || busy === 'logout' || busy === 'install'">
      <div class="provider-summary-row">
        <span class="provider-summary-label">{{ t('app.providers.resources.cli') }}</span>
        <div class="provider-summary-main">
          <div class="provider-summary-line">
            <span class="provider-summary-status" :class="{ 'is-busy': busy === 'install' || initialChecking }">
              <i v-if="busy === 'install' || initialChecking" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
              {{ cliStatusText }}
            </span>
            <span v-if="cliValue" class="provider-summary-value" :title="cliValue">· {{ cliValue }}</span>
          </div>
          <p v-if="busy === 'install'" class="provider-summary-note" role="status">{{ t('app.providers.installPending') }}</p>
          <p v-else-if="status.detail && status.installed !== true" class="provider-summary-note">{{ status.detail }}</p>
        </div>
        <div class="provider-summary-actions">
          <button type="button" class="btn btn-sm btn-outline-primary provider-tool-btn" :disabled="initialChecking || busy === 'install'" @click="cliMaintenanceOpen = true">{{ t(initialChecking ? 'app.providers.checking' : status.installed ? 'app.cliMaintenance.check' : 'app.providers.install') }}</button>
        </div>
      </div>
      <div class="provider-summary-row">
        <span class="provider-summary-label">{{ t('app.providers.resources.account') }}</span>
        <div class="provider-summary-main">
          <div class="provider-summary-line">
            <span class="provider-summary-status" :class="{ 'is-busy': loginBusy || busy === 'logout' }">
              <i v-if="loginBusy || busy === 'logout'" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
              {{ accountBadge }}
            </span>
            <span v-if="accountValue" class="provider-summary-value" :title="accountValue">· {{ accountValue }}</span>
          </div>
          <p v-if="phaseNote" class="provider-summary-note">{{ phaseNote }}</p>
          <p v-else-if="status.loginError" class="provider-summary-note">{{ status.loginError }}</p>
          <p v-else-if="status.installed && accountState === 'unknown' && status.detail" class="provider-summary-note">{{ status.detail }}</p>
        </div>
        <div v-if="status.installed" class="provider-summary-actions">
          <ProviderRowActions
            v-if="accountState === 'signed_in' && !loginBusy"
            trigger="text"
            :menu-label="t('app.providers.resourceActions.accountMenu')"
            :items="accountMenuItems"
            @select="onAccountAction"
          />
          <template v-else>
            <button type="button" class="btn btn-sm btn-primary provider-tool-btn" :disabled="controlsLocked || loginBusy" @click="emit('login', 'github')">
              <i v-if="loginKind === 'github'" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
              {{ loginKind === 'github' ? t('app.providers.signingIn') : accountState === 'expired' ? t('app.providers.reauth') : t('app.providers.signInGitHub') }}
            </button>
            <button type="button" class="btn btn-sm btn-outline-primary provider-tool-btn" :disabled="controlsLocked || loginBusy" @click="emit('login', 'microsoft')">
              <i v-if="loginKind === 'microsoft'" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
              {{ loginKind === 'microsoft' ? t('app.providers.signingIn') : t('app.providers.signInMicrosoft') }}
            </button>
            <button v-if="loginBusy" type="button" class="btn btn-sm btn-outline-secondary provider-tool-btn" @click="emit('cancel-login')">
              {{ t('app.providers.cancel') }}
            </button>
            <button v-if="loginBusy && status.loginUrl" type="button" class="btn btn-sm btn-outline-secondary provider-tool-btn" @click="emit('open-login-url', status.loginUrl)">
              {{ t('app.providers.openBrowser') }}
            </button>
          </template>
        </div>
      </div>
    </section>

    <section class="provider-card provider-resource-card">
      <ProviderResourceToolbar
        :title="t('app.providers.resources.tunnelIds')"
        :total-count="totalCount"
        :visible-count="signedIn ? visibleItems.length : 0"
        :search-query="searchQuery"
        :search-placeholder="t('app.providers.resources.searchResources')"
        @update-search-query="emit('update-search-query', $event)"
      >
        <template #actions>
          <button type="button" class="btn btn-sm btn-outline-secondary provider-tool-btn" :disabled="resourceBusy === 'refresh' || !signedIn" :aria-busy="resourceBusy === 'refresh'" @click="emit('refresh-resources')">
            <i v-if="resourceBusy === 'refresh'" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
            {{ t('app.providers.refreshIds') }}
          </button>
          <button type="button" class="btn btn-sm btn-primary provider-tool-btn" :disabled="!canCreate" @click="openCreate">+ {{ t('app.providers.newId') }}</button>
        </template>
      </ProviderResourceToolbar>
        <p v-if="showResourceError" class="provider-state">
          {{ resources.detail || t('app.providers.loadFailed') }}
          <button type="button" class="btn btn-link btn-sm devtunnel-inline-action" @click="emit('refresh-resources')">{{ t('app.providers.retry') }}</button>
          <span v-if="items.length">{{ t('app.providers.resources.stale') }}</span>
        </p>
        <div class="provider-table-wrap" :class="{ 'provider-table-empty': !signedIn || !visibleItems.length }">
          <table class="table align-middle mb-0 provider-table provider-dev-table">
            <colgroup>
              <col class="provider-col-id" />
              <col class="provider-col-region" />
              <col class="provider-col-port" />
              <col class="provider-col-access" />
              <col class="provider-col-links" />
              <col class="provider-col-actions" />
            </colgroup>
            <thead>
              <tr>
                <th>{{ t('app.providers.idColumn') }}</th>
                <th>{{ t('app.providers.regionColumn') }}</th>
                <th>{{ t('app.providers.portColumn') }}</th>
                <th>{{ t('app.modals.tunnel.access') }}</th>
                <th>{{ t('app.providers.resources.linkedColumn') }}</th>
                <th class="text-end">{{ t('app.tunnels.table.action') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="initialChecking">
                <td colspan="6" class="provider-state-cell" role="status">
                  <span class="spinner-border spinner-border-sm" aria-hidden="true" />
                  {{ t('app.providers.checking') }}
                </td>
              </tr>
              <tr v-else-if="!signedIn">
                <td colspan="6" class="provider-state-cell" role="status">{{ t('app.providers.signInToList') }}</td>
              </tr>
              <tr v-else-if="showReading">
                <td colspan="6" class="provider-state-cell" role="status">
                  <span class="spinner-border spinner-border-sm" aria-hidden="true" />
                  {{ t('app.providers.resources.reading') }}
                </td>
              </tr>
              <tr v-else-if="showEmpty">
                <td colspan="6" class="provider-state-cell">{{ t('app.providers.emptyIds') }}</td>
              </tr>
              <tr v-else-if="showNoMatch">
                <td colspan="6" class="provider-state-cell">
                  {{ t('app.providers.resources.noMatch') }}
                  <button type="button" class="btn btn-link btn-sm provider-clear-search" @click="emit('update-search-query', '')">
                    {{ t('app.providers.resources.clearSearch') }}
                  </button>
                </td>
              </tr>
              <tr v-for="item in (signedIn ? visibleItems : [])" :key="item.id">
                <td><TooltipText :text="item.id" class-name="cell-ellipsis provider-main" /></td>
                <td><TooltipText :text="regionText(item.id)" class-name="cell-ellipsis" /></td>
                <td class="provider-port-cell">
                  <TooltipText :text="portText(item)" :title="portTitle(item)" class-name="cell-ellipsis" />
                </td>
                <td>{{ accessLabel(item.access) }}</td>
                <td>
                  <TooltipText
                    :text="linkText(linkedTunnels(item.id).length)"
                    :title="linkedTunnels(item.id).map((tunnel) => tunnel.name).filter(Boolean).join(', ') || linkText(0)"
                    :class-name="linkedTunnels(item.id).length ? 'cell-ellipsis' : 'cell-ellipsis provider-muted'"
                  />
                </td>
                <td class="text-end provider-actions-cell">
                  <ProviderRowActions
                    :items="resourceMenuItems(item)"
                    :menu-label="t('app.providers.resourceActions.more')"
                    @select="onResourceAction($event, item)"
                  />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
    </section>
  </div>

  <Teleport to="body">
    <div v-if="createOpen" class="devtunnel-resource-overlay">
      <div class="dialog-card compact-dialog devtunnel-resource-dialog" role="dialog" aria-modal="true" aria-labelledby="devtunnel-create-title">
        <div class="dialog-head">
          <h3 id="devtunnel-create-title" class="dialog-title">{{ t('app.providers.createTitle') }}</h3>
        </div>
        <div class="dialog-body">
          <template v-if="createdResource?.id && resourceBusy !== 'create'">
            <div class="copy-dialog-label">{{ t('app.providers.createdId') }}</div>
            <div class="copy-value-row">
              <span class="copy-value-text" :title="createdResource.id">{{ createdResource.id }}</span>
              <CopyIconButton
                :value="createdResource.id"
                :title="t('app.providers.copyId')"
                button-class="devtunnel-detail-action"
                @copied="copyError = ''"
                @error="copyError = t('app.common.copyFailed')"
              />
            </div>
            <p v-if="copyError" class="field-warning mb-0 mt-2">{{ copyError }}</p>
            <div class="devtunnel-actions">
              <button type="button" class="btn btn-sm btn-primary" @click="emit('create-tunnel', createdResource.id); createOpen = false">{{ t('app.providers.useThisId') }}</button>
            </div>
          </template>
          <template v-else>
            <label class="form-label" for="devtunnel-create-id">{{ t('app.providers.idColumn') }}</label>
            <input
              id="devtunnel-create-id"
              v-model="createName"
              class="form-control"
              :class="{ 'is-invalid': Boolean(createNameError) }"
              type="text"
              autocomplete="off"
              autocapitalize="none"
              autocorrect="off"
              spellcheck="false"
              :placeholder="t('app.modals.tunnel.customIdPlaceholder')"
              aria-describedby="devtunnel-create-id-hint devtunnel-create-id-error"
              :aria-invalid="Boolean(createNameError)"
            />
            <div id="devtunnel-create-id-hint" class="field-note">{{ t('app.providers.idCustomHint') }}</div>
            <div v-if="createNameError" id="devtunnel-create-id-error" class="field-warning" aria-live="polite">{{ createNameError }}</div>
            <div class="row g-3 mt-1">
              <div class="col-6">
                <label class="form-label">{{ t('app.modals.tunnel.expiration') }}</label>
                <input v-model.number="createAmount" class="form-control" type="number" min="1" :max="createUnit === 'h' ? 720 : 30" />
              </div>
              <div class="col-6">
                <label class="form-label">{{ t('app.modals.tunnel.expirationUnit') }}</label>
                <select v-model="createUnit" class="form-select">
                  <option value="h">{{ t('app.modals.tunnel.expirationHours') }}</option>
                  <option value="d">{{ t('app.modals.tunnel.expirationDays') }}</option>
                </select>
              </div>
            </div>
            <label class="form-label mt-3">{{ t('app.modals.tunnel.access') }}</label>
            <div class="devtunnel-access">
              <button type="button" class="devtunnel-access-option" :class="{ 'is-selected': createAccess !== 'public' }" @click="createAccess = 'private'">{{ t('app.modals.tunnel.accessPrivate') }}</button>
              <button type="button" class="devtunnel-access-option" :class="{ 'is-selected': createAccess === 'public' }" @click="createAccess = 'public'">{{ t('app.modals.tunnel.accessPublic') }}</button>
            </div>
            <div v-if="createAccess === 'public'" class="field-warning">{{ t('app.modals.tunnel.publicWarning') }}</div>
            <div class="mt-3">
              <RegionDropdown
                v-model="createRegion"
                :regions="regionProbe.regions || []"
                :state="regionProbe.state || 'idle'"
                :loading="!!regionProbe.loading"
                @test="emit('test-regions')"
              />
            </div>
            <p v-if="createError || props.createError" class="form-error mb-0 mt-2">{{ createError || props.createError }}</p>
          </template>
        </div>
        <div class="dialog-footer">
          <button type="button" class="btn btn-outline-secondary" :disabled="resourceBusy === 'create'" @click="createOpen = false">{{ t('app.common.cancel') }}</button>
          <button v-if="!(createdResource?.id && resourceBusy !== 'create')" type="button" class="btn btn-primary" :disabled="resourceBusy === 'create'" @click="submitCreate">
            {{ resourceBusy === 'create' ? t('app.providers.loadingIds') : t('app.providers.newId') }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="deleteTarget" class="devtunnel-resource-overlay">
      <div class="dialog-card compact-dialog devtunnel-resource-dialog" role="dialog" aria-modal="true" aria-labelledby="devtunnel-delete-title" aria-describedby="devtunnel-delete-body">
        <div class="dialog-head">
          <h3 id="devtunnel-delete-title" class="dialog-title">{{ t('app.deletion.idDeleteTitle') }}</h3>
        </div>
        <div class="dialog-body">
          <div class="provider-delete-target">
            <div class="copy-dialog-label">{{ t('app.providers.idColumn') }}</div>
            <div class="provider-delete-id">{{ deleteTarget.id }}</div>
          </div>
          <p id="devtunnel-delete-body" class="mb-0">{{ t('app.deletion.idEffect') }}</p>
          <p v-if="deletePreview?.blocked" class="form-error mb-0 mt-2">{{ t('app.deletion.linkedBlock', { names: (deletePreview.linkedNames || []).join('、') }) }}</p>
          <p v-if="deleteError" class="form-error mb-0 mt-2">{{ deleteError }}</p>
        </div>
        <div class="dialog-footer">
          <button type="button" class="btn btn-outline-secondary" :disabled="resourceBusy === 'delete'" @click="deleteTarget = null">{{ t('app.common.cancel') }}</button>
          <button v-if="deleteRetryId" type="button" class="btn btn-outline-secondary" :disabled="resourceBusy === 'delete'" @click="emit('retry-cleanup', deleteRetryId)">{{ t('app.deletion.retry') }}</button>
          <button type="button" class="btn btn-danger" :disabled="resourceBusy === 'delete' || deletePreview?.blocked" @click="emit('delete-resource', { id: deleteTarget.id, cleanupDNS: false })">{{ resourceBusy === 'delete' ? t('app.deletion.deleting') : t('app.providers.deleteConfirm') }}</button>
        </div>
      </div>
    </div>

    <div v-if="detailOpen" class="devtunnel-resource-overlay">
      <div class="dialog-card compact-dialog devtunnel-resource-dialog devtunnel-detail-dialog" role="dialog" aria-modal="true" aria-labelledby="devtunnel-detail-title">
        <div class="dialog-head">
          <h3 id="devtunnel-detail-title" class="dialog-title">{{ t('app.providers.detailTitle') }}</h3>
        </div>
        <div class="dialog-body">
          <p v-if="detailResource?.state === 'loading'" class="field-note">{{ t('app.providers.loadingIds') }}</p>
          <template v-else-if="detailResource?.resource">
            <dl class="devtunnel-detail-fields">
              <dt>{{ t('app.providers.idColumn') }}</dt>
              <dd>
                <div class="copy-value-row">
                  <span class="copy-value-text" :title="detailResource.resource.id">{{ detailResource.resource.id }}</span>
                  <CopyIconButton
                    button-class="devtunnel-detail-action"
                    :value="detailResource.resource.id"
                    :title="t('app.providers.copyId')"
                    @copied="detailCopyTarget = ''"
                    @error="detailCopyTarget = 'id'"
                  />
                </div>
                <p v-if="detailCopyTarget === 'id'" class="field-warning mb-0 mt-2">{{ t('app.common.copyFailed') }}</p>
              </dd>
              <dt>{{ t('app.providers.regionColumn') }}</dt>
              <dd>{{ regionText(detailResource.resource.id) }}</dd>
              <dt>{{ t('app.modals.tunnel.access') }}</dt>
              <dd>{{ accessLabel(detailResource.resource.access) }}</dd>
              <dt>{{ t('app.modals.tunnel.expiration') }}</dt>
              <dd>{{ detailResource.resource.expiration || '—' }}</dd>
              <dt>{{ t('app.providers.portColumn') }}</dt>
              <dd>{{ portLabel(detailResource.resource) }}</dd>
              <dt>{{ t('app.providers.localTunnel') }}</dt>
              <dd>{{ linkedTunnels(detailResource.resource.id).map(tunnel => tunnel.name).join(', ') || t('app.providers.noLocalTunnel') }}</dd>
            </dl>
            <div v-if="detailPublicURLs.length" class="devtunnel-detail-urls">
              <div v-for="url in detailPublicURLs" :key="url" class="copy-value-row">
                <span class="copy-value-text" :title="url">{{ url }}</span>
                <CopyIconButton
                  button-class="devtunnel-detail-action"
                  :value="url"
                  :title="t('app.tunnels.actions.copyUrl')"
                  @copied="detailCopyTarget = ''"
                  @error="detailCopyTarget = 'url'"
                />
                <IconActionButton
                  button-class="devtunnel-detail-action"
                  :title="t('app.tunnels.actions.openUrl')"
                  :aria-label="t('app.tunnels.actions.openUrl')"
                  icon-class="bi-box-arrow-up-right"
                  @click="emit('open-url', url)"
                />
              </div>
              <p v-if="detailCopyTarget === 'url'" class="field-warning mb-2">{{ t('app.common.copyFailed') }}</p>
              <p class="field-note mb-0">{{ t('app.providers.urlNotLive') }}</p>
            </div>
            <p v-if="detailResource.state && detailResource.state !== 'ready'" class="form-error">{{ detailResource.detail }}</p>
          </template>
          <p v-else class="form-error mb-0">{{ detailResource?.detail || t('app.providers.loadFailed') }}</p>
        </div>
        <div class="dialog-footer">
          <button type="button" class="btn btn-sm btn-primary" @click="detailOpen = false">{{ t('app.common.close') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
