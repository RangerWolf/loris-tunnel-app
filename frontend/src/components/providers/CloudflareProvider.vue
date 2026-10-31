<script setup>
import CLIUpdateDialog from '../common/CLIUpdateDialog.vue'
import { removeCloudflareAuthorization } from '../../utils/cloudflare-authorizations'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TooltipText from '../common/TooltipText.vue'
import IconActionButton from '../common/IconActionButton.vue'
import CopyIconButton from '../common/CopyIconButton.vue'
import ProviderResourceToolbar from '../common/ProviderResourceToolbar.vue'
import ProviderRowActions from '../common/ProviderRowActions.vue'
import { writeTextToClipboard } from '../../utils/clipboard'
import { normalizeHostname, shortTunnelID } from '../../utils/cloudflare'
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
  zones: { type: Object, default: () => ({ state: 'idle', detail: '', items: [] }) },
  searchQuery: { type: String, default: '' },
})

const emit = defineEmits([
  'refresh',
  'install',
  'cancel-install',
  'login',
  'cancel-login',
  'open-login-url',
  'open-docs',
  'refresh-resources',
  'refresh-zones',
  'authorizations-changed',
  'create-resource',
  'delete-resource',
  'retry-cleanup',
  'create-tunnel',
  'inspect-resource',
  'reset-created',
  'update-search-query',
])
const { t } = useI18n()
const cliMaintenanceOpen = ref(false)

const accountState = computed(() => props.status?.accountState || 'unknown')
const loginKind = computed(() => (String(props.busy).startsWith('login:') ? String(props.busy).slice(6) : ''))
const loginBusy = computed(() => props.status?.loginInProgress || (loginKind.value !== '' && loginKind.value !== 'cancel'))
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
const controlsLocked = computed(() => {
  const value = String(props.busy || '')
  return props.status?.checking || (value !== '' && value !== 'checked')
})
const checkLabel = computed(() => {
  if (props.busy === 'check' || initialChecking.value) return t('app.providers.checking')
  if (props.busy === 'checked') return t('app.providers.checked')
  return t('app.providers.check')
})
function loginErrorText(error) {
  if (String(error).startsWith('cloudflare_authorization_')) return t(`app.cloudflare.authorizationErrors.${String(error).slice(25)}`)
  if (error === 'cloudflare_login_incomplete') return t('app.cloudflare.loginIncomplete')
  if (error === 'cloudflare_login_incomplete_kept') return t('app.cloudflare.loginIncompleteKept')
  if (error === 'cloudflare_login_existing_cert') return t('app.cloudflare.loginExistingCert')
  return error
}

const accountBadge = computed(() => {
  if (initialChecking.value) return t('app.providers.checking')
  if (loginBusy.value) return t('app.cloudflare.signingIn')
  if (accountState.value === 'signed_in') return t('app.cloudflare.authenticated')
  if (accountState.value === 'expired') return t('app.providers.expired')
  if (accountState.value === 'signed_out') return t('app.cloudflare.notAuthenticated')
  return t('app.providers.unknown')
})
const zoneNames = computed(() => (props.zones?.items || []).map((zone) => String(zone?.name || '').trim()).filter(Boolean))
const domainsOpen = ref(false)
const domainQuery = ref('')
const domainRemoveTarget = ref(null)
const domainError = ref('')
const domainRemoving = ref(false)
const domainSearchInput = ref(null)
const domainManageButton = ref(null)
const domainRows = computed(() => (props.zones?.items || []).filter(zone => String(zone.name || zone.id).toLowerCase().includes(domainQuery.value.trim().toLowerCase())))
function openDomains() {
  domainQuery.value = ''
  domainError.value = ''
  domainRemoveTarget.value = null
  domainsOpen.value = true
  emit('refresh-zones')
  nextTick(() => domainSearchInput.value?.focus())
}
function closeDomains() {
  if (domainRemoving.value) return
  domainsOpen.value = false
  nextTick(() => domainManageButton.value?.focus())
}
function reauthorizeDomain(zone) {
  closeDomains()
  emit('login', zone.id)
}
function domainBlocked(zone) { return zone.linkedTunnels > 0 || zone.pendingCleanup }
function domainRemoveHint(zone) {
  if (zone.linkedTunnels > 0) return t(zone.pendingCleanup ? 'app.cloudflare.domainRemoveLinkedCleanupBlocked' : 'app.cloudflare.domainRemoveBlocked', { count: zone.linkedTunnels })
  if (zone.pendingCleanup) return t('app.cloudflare.domainRemoveCleanupBlocked')
  return t('app.cloudflare.removeDomain')
}
async function confirmRemoveDomain() {
  if (!domainRemoveTarget.value || domainRemoving.value) return
  domainRemoving.value = true
  domainError.value = ''
  try {
    await removeCloudflareAuthorization(domainRemoveTarget.value.id)
    domainRemoveTarget.value = null
    emit('authorizations-changed')
  } catch (err) {
    const code = String(err?.message || err)
    domainError.value = code.startsWith('cloudflare_authorization_') ? loginErrorText(code) : t('app.cloudflare.domainOperationFailed')
  } finally { domainRemoving.value = false }
}
function domainsKeydown(event) {
  if (event.key === 'Escape') { event.stopPropagation(); closeDomains() }
  if (event.key === 'Tab') {
    const controls = [...event.currentTarget.querySelectorAll('button:not([disabled]), input:not([disabled]), [tabindex="0"]')]
    const first = controls[0], last = controls[controls.length - 1]
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
  }
}
const zoneState = computed(() => props.zones?.state || 'idle')
const localDomains = computed(() => {
  const result = new Map()
  for (const tunnel of props.tunnels) {
    if (tunnel.provider !== 'cloudflare') continue
    const id = String(tunnel.cloudflare?.tunnelId || '').trim().toLowerCase()
    const host = normalizeHostname(tunnel.cloudflare?.hostname).replace(/\.+$/, '')
    if (!id || !host) continue
    if (!result.has(id)) result.set(id, new Set())
    result.get(id).add(host)
  }
  return new Map([...result].map(([id, hosts]) => [id, [...hosts]]))
})
function domainsFor(id) {
  return localDomains.value.get(String(id || '').trim().toLowerCase()) || []
}
const items = computed(() => props.resources?.items || [])
const resourceState = computed(() => props.resources?.state || 'idle')
const signedIn = computed(() => accountState.value === 'signed_in')
const visibleItems = computed(() => {
  const query = String(props.searchQuery || '').trim().toLowerCase()
  if (!query) return items.value
  return items.value.filter((item) => {
    const id = String(item?.id || '').toLowerCase()
    const name = String(item?.name || '').toLowerCase()
    return id.includes(query) || name.includes(query) || domainsFor(item.id).some((host) => host.includes(query))
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
const createName = ref('')
const createError = ref('')
const deleteTarget = ref(null)
const deleteError = ref('')
const deletePreview = ref(null)
const deleteRetryId = ref('')
const cleanupDNS = ref(true)
const detailOpen = ref(false)
const copyError = ref('')
const confirmButton = ref(null)
const createdReady = computed(() => Boolean(props.createdResource?.id) && props.resourceBusy !== 'create')
function showCopyError() { copyError.value = t('app.common.copyFailed') }
function clearCopyError() { copyError.value = '' }
watch(createdReady, (ready) => {
  copyError.value = ''
  if (!ready) return
  nextTick(() => confirmButton.value?.focus())
})
const copiedId = ref('')
let copyFeedbackTimer = null
onBeforeUnmount(() => window.clearTimeout(copyFeedbackTimer))

function linkedTunnels(id) {
  const target = String(id || '').toLowerCase()
  return props.tunnels.filter((tunnel) => (tunnel.provider || 'ssh') === 'cloudflare' && String(tunnel?.cloudflare?.tunnelId || '').toLowerCase() === target)
}

function linkText(count) {
  return count ? t('app.providers.resources.linkedCount', { count }) : t('app.providers.resources.unlinked')
}

function openCreate() {
  createName.value = ''
  createError.value = ''
  createOpen.value = true
  emit('reset-created')
}

function submitCreate() {
  createError.value = ''
  emit('create-resource', { name: createName.value.trim() })
}

watch(() => props.createdResource, (resource) => {
  if (resource?.id && createOpen.value) createError.value = ''
})
watch(() => props.createError, (value) => {
  if (value) createError.value = value
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
  detailOpen.value = true
  emit('inspect-resource', item.id)
}

async function copyId(id) {
  const value = String(id || '')
  if (!value || !(await writeTextToClipboard(value))) return
  copiedId.value = value
  window.clearTimeout(copyFeedbackTimer)
  copyFeedbackTimer = window.setTimeout(() => { copiedId.value = '' }, 1500)
}

function resourceMenuItems(item) {
  return [
    { key: 'details', label: t('app.providers.resourceActions.details') },
    { key: 'copy', label: copiedId.value === item.id ? t('app.providers.copiedId') : t('app.providers.resourceActions.copyId') },
    { key: 'create', label: t('app.providers.resourceActions.useId'), disabled: !item.credentialReady },
    { key: 'delete', label: t('app.providers.resourceActions.deleteId'), danger: true, separatorBefore: true },
  ]
}

function onResourceAction(key, item) {
  if (key === 'details') openDetail(item)
  else if (key === 'copy') copyId(item.id)
  else if (key === 'create') {
    if (!item.credentialReady) return
    emit('create-tunnel', item.id)
  } else if (key === 'delete') {
    deleteTarget.value = item
    deleteError.value = ''
    deletePreview.value = null
    deleteRetryId.value = ''
    cleanupDNS.value = true
    PreviewProviderResourceDeletion('cloudflare', item.id).then((preview) => {
      if (deleteTarget.value?.id === item.id) deletePreview.value = preview
    }).catch((err) => {
      if (deleteTarget.value?.id === item.id) deleteError.value = err?.message || String(err)
    })
  }
}

const detailLinked = computed(() => linkedTunnels(props.detailResource?.resource?.id)[0])
</script>

<template>
  <CLIUpdateDialog v-if="cliMaintenanceOpen" provider="cloudflare" @close="cliMaintenanceOpen = false" @changed="emit('refresh')" />
  <div class="provider-stack">
    <section class="provider-summary" :aria-busy="loginBusy || busy === 'install'">
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
        <span class="provider-summary-label">{{ t('app.cloudflare.authentication') }}</span>
        <div class="provider-summary-main">
          <div class="provider-summary-line">
            <span class="provider-summary-status" :class="{ 'is-busy': loginBusy }">
              <i v-if="loginBusy" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
              {{ accountBadge }}
            </span>
          </div>
          <div v-if="(signedIn || zones.items?.length) && !loginBusy" class="provider-summary-note" role="status">
            <template v-if="zoneState === 'loading' || zoneState === 'idle'">{{ t('app.cloudflare.zonesReading') }}</template>
            <template v-else-if="zoneState !== 'ready'">{{ t('app.cloudflare.zonesFailed') }}</template>
            <template v-else>
              <span>{{ t('app.cloudflare.availableZones') }}（{{ zoneNames.length }}）</span>
              <span class="cloudflare-zone-tags">
                <span v-for="name in zoneNames.slice(0, 2)" :key="name" class="cloudflare-zone-tag" :title="name">{{ name }}</span>
                <span v-if="!zoneNames.length">—</span>
                <button v-if="zoneNames.length > 2" type="button" class="cloudflare-zone-more" :aria-label="t('app.cloudflare.manageDomains')" @click="openDomains">… +{{ zoneNames.length - 2 }}</button>
              </span>
            </template>
            <IconActionButton
              button-class="btn-link provider-zone-refresh"
              :title="t('app.cloudflare.refreshZones')"
              :aria-label="t('app.cloudflare.refreshZones')"
              :icon-class="zoneState === 'loading' ? 'bi-arrow-clockwise devtunnel-spin' : 'bi-arrow-clockwise'"
              :disabled="zoneState === 'loading' || controlsLocked"
              :aria-busy="zoneState === 'loading'"
              @click="emit('refresh-zones')"
            />
          </div>
          <p v-if="loginBusy" class="provider-summary-note">{{ t('app.cloudflare.waitingLogin') }}</p>
          <p v-else-if="status.loginError" class="provider-summary-note">{{ loginErrorText(status.loginError) }}</p>
          <p v-else-if="status.loginResult" class="provider-summary-note" role="status">{{ t(status.loginResult === 'updated' ? 'app.cloudflare.domainUpdated' : 'app.cloudflare.domainAdded') }}</p>
          <p v-else-if="!initialChecking && accountState !== 'signed_in'" class="provider-summary-note">{{ t('app.cloudflare.connectHint') }}</p>
        </div>
        <div v-if="status.installed" class="provider-summary-actions">
          <button v-if="accountState === 'signed_in' && !loginBusy" type="button" class="btn btn-sm btn-outline-primary provider-tool-btn" :disabled="controlsLocked" @click="emit('login')">
            {{ t('app.cloudflare.addDomain') }}
          </button>
          <button v-if="!loginBusy && (signedIn || zones.items?.length)" ref="domainManageButton" type="button" class="btn btn-sm btn-outline-secondary provider-tool-btn" :disabled="controlsLocked" @click="openDomains">{{ t('app.cloudflare.manageDomains') }}</button>
          <template v-if="!signedIn || loginBusy">
            <button type="button" class="btn btn-sm btn-primary provider-tool-btn" :disabled="controlsLocked || loginBusy" @click="emit('login')">
              <i v-if="loginBusy" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
              {{ loginBusy ? t('app.cloudflare.signingIn') : accountState === 'expired' ? t('app.providers.reauth') : t('app.cloudflare.signIn') }}
            </button>
            <button v-if="loginBusy" type="button" class="btn btn-sm btn-outline-secondary provider-tool-btn" @click="emit('cancel-login')">{{ t('app.providers.cancel') }}</button>
            <button v-if="loginBusy && status.loginUrl" type="button" class="btn btn-sm btn-outline-secondary provider-tool-btn" @click="emit('open-login-url', status.loginUrl)">{{ t('app.providers.openBrowser') }}</button>
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
          {{ resources.detail && resources.detail !== 'cloudflare_list_invalid_response' ? resources.detail : t('app.providers.loadFailed') }}
          <button type="button" class="btn btn-link btn-sm devtunnel-inline-action" @click="emit('refresh-resources')">{{ t('app.providers.retry') }}</button>
          <span v-if="items.length">{{ t('app.providers.resources.stale') }}</span>
        </p>
        <div class="provider-table-wrap" :class="{ 'provider-table-empty': !signedIn || !visibleItems.length }">
          <table class="table align-middle mb-0 provider-table provider-cf-table">
            <colgroup>
              <col class="provider-col-cf-name" />
              <col class="provider-col-domains" />
              <col class="provider-col-credential" />
              <col class="provider-col-links" />
              <col class="provider-col-actions" />
            </colgroup>
            <thead>
              <tr>
                <th>{{ t('app.providers.resources.nameAndId') }}</th>
                <th>{{ t('app.cloudflare.localDomains') }}</th>
                <th>{{ t('app.providers.resources.credential') }}</th>
                <th>{{ t('app.providers.resources.linkedColumn') }}</th>
                <th class="text-end">{{ t('app.tunnels.table.action') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="initialChecking">
                <td colspan="5" class="provider-state-cell" role="status">
                  <span class="spinner-border spinner-border-sm" aria-hidden="true" />
                  {{ t('app.providers.checking') }}
                </td>
              </tr>
              <tr v-else-if="!signedIn">
                <td colspan="5" class="provider-state-cell" role="status">{{ t('app.cloudflare.signInToList') }}</td>
              </tr>
              <tr v-else-if="showReading">
                <td colspan="5" class="provider-state-cell" role="status">
                  <span class="spinner-border spinner-border-sm" aria-hidden="true" />
                  {{ t('app.providers.resources.reading') }}
                </td>
              </tr>
              <tr v-else-if="showEmpty">
                <td colspan="5" class="provider-state-cell">{{ t('app.providers.emptyIds') }}</td>
              </tr>
              <tr v-else-if="showNoMatch">
                <td colspan="5" class="provider-state-cell">
                  {{ t('app.providers.resources.noMatch') }}
                  <button type="button" class="btn btn-link btn-sm provider-clear-search" @click="emit('update-search-query', '')">
                    {{ t('app.providers.resources.clearSearch') }}
                  </button>
                </td>
              </tr>
              <tr v-for="item in (signedIn ? visibleItems : [])" :key="item.id">
                <td>
                  <TooltipText :text="item.name || '—'" class-name="cell-ellipsis provider-name" />
                  <TooltipText :text="shortTunnelID(item.id)" :title="item.id" class-name="cell-ellipsis provider-sub" />
                </td>
                <td>
                  <TooltipText v-for="host in domainsFor(item.id).slice(0, 2)" :key="host" :text="host" class-name="cell-ellipsis provider-domain" />
                  <button v-if="domainsFor(item.id).length > 2" type="button" class="btn btn-link btn-sm provider-more-domains" @click="openDetail(item)">{{ t('app.cloudflare.moreDomains', { count: domainsFor(item.id).length - 2 }) }}</button>
                  <TooltipText v-if="!domainsFor(item.id).length" text="—" :title="t('app.cloudflare.noLocalDomains')" class-name="provider-muted" />
                </td>
                <td>{{ item.credentialReady ? t('app.providers.resources.credentialReady') : t('app.providers.resources.credentialMissing') }}</td>
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
    <div v-if="domainsOpen" class="devtunnel-resource-overlay" @click.self="closeDomains">
      <div class="dialog-card compact-dialog devtunnel-resource-dialog cloudflare-domains-dialog" role="dialog" aria-modal="true" aria-labelledby="cloudflare-domains-title" @keydown="domainsKeydown">
        <div class="dialog-head"><h3 id="cloudflare-domains-title" class="dialog-title">{{ t('app.cloudflare.manageDomains') }}</h3></div>
        <div class="dialog-body">
          <input ref="domainSearchInput" v-model="domainQuery" type="search" class="form-control" autocapitalize="none" autocorrect="off" spellcheck="false" :placeholder="t('app.cloudflare.searchDomains')" :aria-label="t('app.cloudflare.searchDomains')" />
          <p v-if="zoneState === 'loading'" class="field-note mt-2" role="status">{{ t('app.cloudflare.zonesReading') }}</p>
          <p v-else-if="zoneState !== 'ready'" class="form-error mt-2" role="alert">{{ t('app.cloudflare.zonesFailed') }} <button type="button" class="btn btn-link btn-sm" @click="emit('refresh-zones')">{{ t('app.providers.retry') }}</button></p>
          <div class="cloudflare-domains-list">
            <div v-for="zone in domainRows" :key="zone.id" class="cloudflare-domain-row">
              <div class="cloudflare-domain-info">
                <strong class="cloudflare-domain-name" :title="zone.name || zone.id">{{ zone.name || zone.id }}</strong>
                <span class="field-note" :class="{ 'field-warning': zone.state && zone.state !== 'ready' }">{{ t(!zone.state || zone.state === 'ready' ? 'app.cloudflare.ready' : zone.state === 'network' ? 'app.cloudflare.domainNetwork' : 'app.cloudflare.domainNeedsAuth') }} · {{ linkText(zone.linkedTunnels || 0) }}</span>
                <span v-if="zone.pendingCleanup" class="field-note">{{ t('app.cloudflare.domainCleanupPending') }}</span>
              </div>
              <div class="cloudflare-domain-actions">
                <button type="button" class="btn btn-sm btn-outline-primary" :disabled="controlsLocked || domainRemoving" @click="reauthorizeDomain(zone)">{{ t('app.cloudflare.reauthorizeDomain') }}</button>
                <TooltipText :text="t('app.cloudflare.removeDomain')" :title="domainRemoveHint(zone)" class-name="d-inline-block" :tabindex="domainBlocked(zone) ? 0 : undefined" :aria-label="domainBlocked(zone) ? domainRemoveHint(zone) : undefined">
                  <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="domainBlocked(zone) || controlsLocked || domainRemoving" @click="domainRemoveTarget = zone; domainError = ''">{{ t('app.cloudflare.removeDomain') }}</button>
                </TooltipText>
              </div>
            </div>
            <p v-if="zoneState === 'ready' && !domainRows.length" class="field-note mt-3">{{ t('app.cloudflare.noDomains') }}</p>
          </div>
          <div v-if="domainRemoveTarget" class="cloudflare-domain-confirm" role="alert">
            <p>{{ t('app.cloudflare.removeDomainConfirm', { name: domainRemoveTarget.name || domainRemoveTarget.id }) }}</p>
            <button type="button" class="btn btn-sm btn-outline-secondary me-2" :disabled="domainRemoving" @click="domainRemoveTarget = null">{{ t('app.providers.cancel') }}</button>
            <button type="button" class="btn btn-sm btn-danger" :disabled="domainRemoving" @click="confirmRemoveDomain">{{ t('app.cloudflare.removeDomain') }}</button>
          </div>
          <p v-if="domainError" class="form-error mt-2" role="alert">{{ domainError }}</p>
        </div>
        <div class="dialog-footer">
          <button type="button" class="btn btn-outline-secondary" :disabled="domainRemoving" @click="closeDomains">{{ t('app.common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="controlsLocked || domainRemoving" @click="closeDomains(); emit('login')">{{ t('app.cloudflare.addDomain') }}</button>
        </div>
      </div>
    </div>

    <div v-if="createOpen" class="devtunnel-resource-overlay">
      <div class="dialog-card compact-dialog devtunnel-resource-dialog" role="dialog" aria-modal="true" aria-labelledby="cloudflare-create-title">
        <div class="dialog-head">
          <h3 id="cloudflare-create-title" class="dialog-title">{{ createdReady ? t('app.cloudflare.createdTitle') : t('app.cloudflare.createTitle') }}</h3>
        </div>
        <div class="dialog-body">
          <div v-if="createdReady" class="copy-dialog-fields">
            <div>
              <div class="copy-dialog-label">{{ t('app.cloudflare.tunnelName') }}</div>
              <p class="copy-dialog-value">{{ createdResource.name }}</p>
            </div>
            <div>
              <div id="cloudflare-created-id-label" class="copy-dialog-label">{{ t('app.cloudflare.tunnelId') }}</div>
              <div class="copy-value-row">
                <span id="cloudflare-created-id" class="copy-value-text" :title="createdResource.id" aria-labelledby="cloudflare-created-id-label">{{ createdResource.id }}</span>
                <CopyIconButton :value="createdResource.id" :title="t('app.cloudflare.copyId')" button-class="devtunnel-detail-action" @copied="clearCopyError" @error="showCopyError" />
              </div>
              <p v-if="copyError" class="field-warning mb-0 mt-2">{{ copyError }}</p>
            </div>
          </div>
          <template v-else>
            <label class="form-label" for="cloudflare-create-name">{{ t('app.cloudflare.tunnelName') }}</label>
            <input id="cloudflare-create-name" v-model="createName" class="form-control" type="text" autocapitalize="none" autocorrect="off" spellcheck="false" />
            <p class="field-note mb-0 mt-3">{{ t('app.cloudflare.idAssigned') }}</p>
            <p v-if="createError" class="field-warning mt-2">{{ createError }}</p>
          </template>
        </div>
        <div class="dialog-footer" :class="{ 'copy-dialog-success-footer': createdReady }">
          <template v-if="createdReady">
            <button type="button" class="btn btn-outline-secondary" :disabled="!createdResource.credentialReady" @click="emit('create-tunnel', createdResource.id); createOpen = false">{{ t('app.cloudflare.useThis') }}</button>
            <button ref="confirmButton" type="button" class="btn btn-primary" @click="createOpen = false">{{ t('app.common.confirm') }}</button>
          </template>
          <template v-else>
            <button type="button" class="btn btn-outline-secondary" :disabled="resourceBusy === 'create'" @click="createOpen = false">{{ t('app.common.cancel') }}</button>
            <button type="button" class="btn btn-primary" :disabled="resourceBusy === 'create' || !createName.trim()" @click="submitCreate">{{ t('app.cloudflare.create') }}</button>
          </template>
        </div>
      </div>
    </div>
    <div v-if="deleteTarget" class="devtunnel-resource-overlay">
      <div class="dialog-card compact-dialog devtunnel-resource-dialog" role="dialog" aria-modal="true" aria-labelledby="cloudflare-delete-title" aria-describedby="cloudflare-delete-body cloudflare-delete-dns">
        <div class="dialog-head"><h3 id="cloudflare-delete-title" class="dialog-title">{{ t('app.deletion.idDeleteTitle') }}</h3></div>
        <div class="dialog-body">
          <div class="provider-delete-target">
            <template v-if="deleteTarget.name">
              <div class="copy-dialog-label">{{ t('app.cloudflare.tunnelName') }}</div>
              <div class="provider-delete-name">{{ deleteTarget.name }}</div>
            </template>
            <div class="copy-dialog-label">{{ t('app.cloudflare.tunnelId') }}</div>
            <div class="provider-delete-id">{{ deleteTarget.id }}</div>
          </div>
          <p id="cloudflare-delete-body" class="mb-0">{{ t('app.deletion.idEffect') }}</p>
          <p v-if="deletePreview?.blocked" class="form-error mb-0 mt-2">{{ t('app.deletion.linkedBlock', { names: (deletePreview.linkedNames || []).join('、') }) }}</p>
          <label v-else class="service-delete-check">
            <input v-model="cleanupDNS" type="checkbox" :disabled="resourceBusy === 'delete'" />
            <span>{{ t('app.deletion.cleanupTunnelDns') }}</span>
          </label>
          <ul v-if="deletePreview?.dnsNames?.length" class="service-delete-names">
            <li v-for="name in deletePreview.dnsNames" :key="name">{{ name }}</li>
          </ul>
          <p class="field-note mb-0 mt-2">{{ t('app.deletion.dnsScope') }}</p>
          <p v-if="deletePreview?.dnsQueryError" class="form-error mb-0 mt-2">{{ t('app.deletion.dnsPermission') }}</p>
          <p v-if="deleteError" class="field-warning mb-0 mt-2">{{ deleteError }}</p>
        </div>
        <div class="dialog-footer">
          <button type="button" class="btn btn-outline-secondary" :disabled="resourceBusy === 'delete'" @click="deleteTarget = null">{{ t('app.common.cancel') }}</button>
          <button v-if="deleteRetryId" type="button" class="btn btn-outline-secondary" :disabled="resourceBusy === 'delete'" @click="emit('retry-cleanup', deleteRetryId)">{{ t('app.deletion.retry') }}</button>
          <button type="button" class="btn btn-danger" :disabled="resourceBusy === 'delete' || deletePreview?.blocked || (!!deletePreview?.dnsQueryError && cleanupDNS)" @click="emit('delete-resource', { id: deleteTarget.id, cleanupDNS })">{{ resourceBusy === 'delete' ? t('app.deletion.deleting') : t('app.cloudflare.deleteId') }}</button>
        </div>
      </div>
    </div>
    <div v-if="detailOpen" class="devtunnel-resource-overlay">
      <div class="dialog-card compact-dialog devtunnel-resource-dialog devtunnel-detail-dialog" role="dialog" aria-modal="true" aria-labelledby="cloudflare-detail-title">
        <div class="dialog-head"><h3 id="cloudflare-detail-title" class="dialog-title">{{ t('app.providers.detailTitle') }}</h3></div>
        <div class="dialog-body">
          <p v-if="!detailResource || detailResource.state === 'loading'" class="field-note mb-0">
            <i class="bi bi-arrow-repeat devtunnel-spin me-1" />{{ t('app.providers.loadingIds') }}
          </p>
          <template v-else-if="detailResource.resource?.id">
            <dl class="devtunnel-detail-fields">
              <dt>{{ t('app.cloudflare.tunnelName') }}</dt>
              <dd>{{ detailResource.resource.name || '—' }}</dd>
              <dt>{{ t('app.cloudflare.tunnelId') }}</dt>
              <dd>
                <div class="copy-value-row">
                  <span class="copy-value-text" :title="detailResource.resource.id">{{ detailResource.resource.id }}</span>
                  <CopyIconButton :value="detailResource.resource.id" :title="t('app.cloudflare.copyId')" button-class="devtunnel-detail-action" @copied="clearCopyError" @error="showCopyError" />
                </div>
                <p v-if="copyError" class="field-warning mb-0 mt-2">{{ copyError }}</p>
              </dd>
              <dt>{{ t('app.cloudflare.created') }}</dt>
              <dd>{{ detailResource.resource.createdAt || '—' }}</dd>
              <dt>{{ t('app.cloudflare.credential') }}</dt>
              <dd>{{ detailResource.resource.credentialReady ? t('app.cloudflare.ready') : t('app.cloudflare.missing') }}</dd>
              <dt>{{ t('app.cloudflare.connections') }}</dt>
              <dd>{{ detailResource.resource.cached ? '—' : (detailResource.resource.connections || 0) }}</dd>
              <dt>{{ t('app.cloudflare.linkedTunnel') }}</dt>
              <dd>{{ detailLinked?.name || '—' }}</dd>
              <dt>{{ t('app.cloudflare.localDomains') }}</dt>
              <dd>
                <div v-for="host in domainsFor(detailResource.resource.id)" :key="host" class="copy-value-row">
                  <span class="copy-value-text">{{ host }}</span>
                  <CopyIconButton :value="host" :title="t('app.cloudflare.copyDomain')" button-class="devtunnel-detail-action" @copied="clearCopyError" @error="showCopyError" />
                </div>
                <span v-if="!domainsFor(detailResource.resource.id).length" :title="t('app.cloudflare.noLocalDomains')">—</span>
                <p class="field-note mb-0 mt-2">{{ t('app.cloudflare.localDomainsSource') }}</p>
              </dd>
            </dl>
            <p v-if="detailResource.state && detailResource.state !== 'ready'" class="field-warning mb-0 mt-2">{{ detailResource.detail }}</p>
          </template>
          <p v-else class="field-warning mb-0">{{ detailResource.detail || t('app.providers.loadFailed') }}</p>
        </div>
        <div class="dialog-footer">
          <button type="button" class="btn btn-primary" @click="detailOpen = false">{{ t('app.common.confirm') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
