<script setup>
import { frpState, refreshFRP } from './utils/frp'
import { frpImportSignature } from './utils/frpc-import'
import { devTunnelServiceOwner, devTunnelNameOwner, sameTunnelID, cloudflareHostnameOwner } from './utils/tunnel-bindings'
import { isDevTunnelNameConflict } from './utils/devtunnel-name'
import { normalizeDevTunnelName, DEV_TUNNEL_NAME_PATTERN } from './utils/devtunnel-name'
import { startCloudflareZoneAuthorization } from './utils/cloudflare-authorizations'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import wailsConfig from '../../wails.json'
import {
  CheckForUpdates as CheckForUpdatesAPI,
  CreateGroup,
  CreateJumper,
  CreateTunnel,
  DeleteGroup,
  DeleteJumper,
  DeleteTunnel,
  PreviewTunnelDeletion,
  DeleteTunnelWithOptions,
  DeleteProviderResourceWithOptions,
  RetryDeletionCleanup,
  ListDeletionTasks,
  DebugJumperFailure as DebugJumperFailureAPI,
  DebugSavedTunnelFailure as DebugSavedTunnelFailureAPI,
  DebugTunnelFailure as DebugTunnelFailureAPI,
  GetAutoRunEnabled,
  GetDistributionChannel,
  GetLicenseStatus as GetLicenseStatusAPI,
  GetSSHConfigImportSources,
  GetStoredLicenseCode,
  GetState,
  GetDevTunnelStatus,
  ListDevTunnelResources,
  GetDevTunnelResource,
  CreateDevTunnelResource,
  LoadDevTunnelRegions,
  InstallDevTunnelCLI,
  CancelDevTunnelInstall,
  CancelDevTunnelLogin,
  StartDevTunnelLogin,
  LogoutDevTunnel,
  GetCloudflareStatus,
  ListCloudflareResources,
  ListCloudflareZones,
  GetCloudflareResource,
  CreateCloudflareResource,
  InstallCloudflareCLI,
  CancelCloudflareInstall,
  CancelCloudflareLogin,
  GetTrafficMonitorEnabled,
  GetTrafficStats,
  LoadSSHConfigJumpersByPath,
  MoveTunnelToGroup,
  OpenReportEmail,
  RedeemLicenseCode as RedeemLicenseCodeAPI,
  ReorderGroups,
  SaveUILocale,
  SetAutoRunEnabled,
  TestJumperConnection as TestJumperConnectionAPI,
  TestTunnelConnection as TestTunnelConnectionAPI,
  ToggleTunnel,
  UpdateGroup,
  UpdateJumper,
  UpdateTunnel,
} from '../wailsjs/go/main/App'
import { devTunnelPortURLs } from './utils/public-urls'
import { regionCreateMessage } from './utils/devtunnel-region'
import { writeTextToClipboard } from './utils/clipboard'
import { isValidTargetHost } from './utils/host-validation'
import { cloudflarePublicURL, isValidHostname, normalizeHostname } from './utils/cloudflare'
import { GetCloudSyncStatus, CheckCloudSyncOnFocus } from './utils/cloud-sync-api'
import { BrowserOpenURL, EventsOn } from '../wailsjs/runtime/runtime'
import {
  trackAppStart,
  trackPageView,
  trackButtonClick,
  trackModalOpen,
  trackModalClose,
  trackTunnelAction,
  trackJumperAction,
} from './utils/analytics'
import AppSidebar from './components/layout/AppSidebar.vue'
import AppTopHeader from './components/layout/AppTopHeader.vue'
import OverviewPage from './components/pages/OverviewPage.vue'
import ProvidersPage from './components/pages/ProvidersPage.vue'
import TunnelsPage from './components/pages/TunnelsPage.vue'
import LogsPage from './components/pages/LogsPage.vue'
import ConfigPage from './components/pages/ConfigPage.vue'
import ProPurchaseDialog from './components/common/ProPurchaseDialog.vue'
import SyncPage from './components/pages/SyncPage.vue'
import AIDebugModal from './components/common/AIDebugModal.vue'
import JumperModal from './components/modals/JumperModal.vue'
import ImportJumperModal from './components/modals/ImportJumperModal.vue'
import TunnelModal from './components/modals/TunnelModal.vue'
import TunnelGroupModal from './components/modals/TunnelGroupModal.vue'
import ImportTunnelModal from './components/modals/ImportTunnelModal.vue'
import './styles/app-shell.css'
import './styles/ssh-ui-m1.css'
import './styles/providers-page.css'
import { AI_DEBUG_ENABLED } from './config/features'

const { t, locale } = useI18n()

const pages = computed(() => [
  {
    key: 'overview',
    title: t('app.sidebar.overview'),
    subtitle: t('app.sidebar.overviewSubtitle'),
    icon: 'bi-speedometer2',
  },
  {
    key: 'providers',
    title: t('app.sidebar.providers'),
    subtitle: t('app.sidebar.providersSubtitle'),
    icon: 'bi-hdd-network',
  },
  { key: 'tunnels', title: t('app.sidebar.tunnels'), subtitle: t('app.sidebar.tunnelsSubtitle'), icon: 'bi-diagram-3' },
  { key: 'sync', title: t('app.sidebar.sync'), subtitle: t('app.sidebar.syncSubtitle'), icon: 'bi-cloud-arrow-up' },
  { key: 'logs', title: t('app.sidebar.logs'), subtitle: t('app.sidebar.logsSubtitle'), icon: 'bi-journal-text' },
  { key: 'config', title: t('app.sidebar.config'), subtitle: t('app.sidebar.configSubtitle'), icon: 'bi-sliders2' },
])

const modeOptions = computed(() => [
  { value: 'local', label: t('app.options.mode.local') },
  { value: 'remote', label: t('app.options.mode.remote') },
  { value: 'dynamic', label: t('app.options.mode.dynamic') },
])

const tunnelDialogModeOptions = computed(() => {
  const provider = tunnelForm.provider || 'ssh'
  if (provider === 'devtunnel') {
    return [
      { value: 'http', label: t('app.options.mode.http') },
      { value: 'https', label: t('app.options.mode.https') },
      { value: 'auto', label: t('app.options.mode.auto') },
    ]
  }
  if (provider === 'frp') return [{ value: 'tcp', label: 'TCP' }, { value: 'http', label: 'HTTP' }]
  if (provider === 'cloudflare') {
    return [
      { value: 'http', label: t('app.options.mode.http') },
      { value: 'https', label: t('app.options.mode.https') },
    ]
  }
  return modeOptions.value
})

const authOptions = computed(() => [
  { value: 'password', label: t('app.options.auth.password') },
  { value: 'ssh_key', label: t('app.options.auth.sshKey') },
  { value: 'ssh_agent', label: t('app.options.auth.sshAgent') },
])

const savedTheme = typeof window !== 'undefined' ? window.localStorage.getItem('lt.theme') : null
const savedAccentColor = typeof window !== 'undefined' ? window.localStorage.getItem('lt.accent') : null
const savedSidebarCollapsed = typeof window !== 'undefined' ? window.localStorage.getItem('lt.sidebar.collapsed') : null
const HIDE_EMPTY_UNGROUPED_STORAGE_KEY = 'lt.tunnel-groups.hide-empty-ungrouped'
const savedHideEmptyUngrouped =
  typeof window !== 'undefined' ? window.localStorage.getItem(HIDE_EMPTY_UNGROUPED_STORAGE_KEY) : null
const theme = ref(savedTheme === 'dark' ? 'dark' : 'light')
const accentColor = ref(savedAccentColor === 'green' ? 'green' : 'blue')
const sidebarCollapsed = ref(savedSidebarCollapsed === '1')
const hideEmptyUngrouped = ref(savedHideEmptyUngrouped !== '0' && savedHideEmptyUngrouped !== 'false')
const activePage = ref('overview')
const providerTab = ref('ssh')
const devTunnelStatus = ref({ checking: true })
const devTunnelBusy = ref('')
const devTunnelResources = reactive({ state: 'idle', detail: '', items: [] })
const devTunnelRegionProbe = reactive({ state: 'idle', detail: '', regions: [], measuredAt: '', cached: false, loading: false })
const devTunnelResourceBusy = ref('')
const devTunnelCreated = ref(null)
const devTunnelCreateError = ref('')
const devTunnelActionResult = ref(null)
const devTunnelDetail = ref(null)
const devTunnelCacheTTL = 60000
const devTunnelDetailCache = new Map()
let devTunnelCacheEpoch = 0
let devTunnelListRequest = null
let devTunnelDetailSeq = 0
let devTunnelResourceCacheAt = 0
let devTunnelResourceSeq = 0
let devTunnelInspectSeq = 0
let devTunnelStatusAt = 0
let devTunnelStatusRequest = null
const cloudflareStatus = ref({ checking: true })
const cloudflareBusy = ref('')
const cloudflareResources = reactive({ state: 'idle', detail: '', items: [] })
const cloudflareZones = reactive({ state: 'idle', detail: '', items: [] })
const cloudflareResourceBusy = ref('')
const cloudflareCreated = ref(null)
const cloudflareCreateError = ref('')
const cloudflareActionResult = ref(null)
const cloudflareDetail = ref(null)
const cloudflareCacheTTL = 60000
const cloudflareDetailCache = new Map()
let cloudflareCacheEpoch = 0
let cloudflareListRequest = null
let cloudflareDetailSeq = 0
let cloudflareResourceCacheAt = 0
let cloudflareZoneCacheAt = 0
let cloudflareZoneRequest = null
let cloudflareZoneSeq = 0
let cloudflareResourceSeq = 0
let cloudflareInspectSeq = 0
let cloudflareStatusAt = 0
let cloudflareStatusRequest = null
const activeSlots = ref(0)
const cloudSyncStatus = ref(null)
const cloudSyncBannerIcons = {
  error: 'bi-exclamation-circle',
  conflict: 'bi-exclamation-triangle',
  update_available: 'bi-info-circle',
}
let stopCloudStatus, stopCloudApplied, stopDevTunnelLogin, stopDevTunnelLoginProgress, stopCloudflareLogin, stopCloudflareLoginProgress
let devTunnelLoginPoll = null
let devTunnelLoginPollInFlight = false
let cloudflareLoginPoll = null
let cloudflareLoginPollInFlight = false
function cloudFocus() {
  void CheckCloudSyncOnFocus().catch(() => {})
}
const selectedLogLevel = ref('all')
const configMessage = ref('')
const initialStateLoading = ref(true)
const DEFAULT_RELEASES_PAGE_URL = 'https://github.com/RangerWolf/loris-tunnel-app/releases'
const releasePageUrl = ref(DEFAULT_RELEASES_PAGE_URL)
const isCheckingUpdates = ref(false)
const isRefreshingLicenseStatus = ref(false)
const updateCheckDialog = reactive({
  visible: false,
  mode: 'idle',
  latestVersion: '',
  message: '',
})
const showConfigToast = ref(false)
const CONFIG_TOAST_DURATION_MS = 3800
let configToastTimer = null

const appMeta = reactive({
  version: wailsConfig?.info?.productVersion || '',
})
const distributionChannel = ref('github')
const isStoreDistribution = computed(() => distributionChannel.value === 'store')
const hasNewVersion = ref(false)
const proLicense = reactive({
  isPro: false,
  expiresAt: '',
  isLifetime: false,
  code: '',
})
const LIFETIME_DURATION_DAYS = 36500
const AI_REPORT_SUPPORT_EMAIL = 'admin@lorisdev.cc'
const AI_REPORT_SUBJECT = '[Loris Tunnel] Report Inappropriate AI Debug Content'
const AI_REPORT_MAX_FIELD_LEN = 800

watchEffect(() => {
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-theme', theme.value)
    document.documentElement.setAttribute('data-bs-theme', theme.value)
    document.documentElement.setAttribute('data-accent', accentColor.value)
  }
})

watch(theme, (newTheme) => {
  if (typeof window !== 'undefined') {
    window.localStorage.setItem('lt.theme', newTheme)
  }
})

watch(accentColor, (color) => {
  if (typeof window !== 'undefined') {
    window.localStorage.setItem('lt.accent', color)
  }
})

watch(sidebarCollapsed, (collapsed) => {
  if (typeof window !== 'undefined') {
    window.localStorage.setItem('lt.sidebar.collapsed', collapsed ? '1' : '0')
  }
})

watch(hideEmptyUngrouped, (enabled) => {
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(HIDE_EMPTY_UNGROUPED_STORAGE_KEY, enabled ? '1' : '0')
  }
})

function toggleSidebar() {
  sidebarCollapsed.value = !sidebarCollapsed.value
}

function setHideEmptyUngrouped(enabled) {
  hideEmptyUngrouped.value = !!enabled
}

watch(configMessage, (message) => {
  if (!message) {
    hideConfigToast()
    return
  }
  showConfigToast.value = true
  if (configToastTimer !== null) {
    window.clearTimeout(configToastTimer)
  }
  configToastTimer = window.setTimeout(() => {
    showConfigToast.value = false
    configToastTimer = null
  }, CONFIG_TOAST_DURATION_MS)
})

watch(activePage, (pageKey) => {
  trackPageView(pageKey, appMeta.version)
})

const jumpers = ref([])
const tunnelGroups = ref([])
const tunnels = ref([])
const jumperSearchQuery = ref('')
const tunnelSearchQuery = ref('')
const STATE_SYNC_INTERVAL_MS = 5000
const TRAFFIC_SYNC_INTERVAL_MS = 1000
const TRAFFIC_HISTORY_LEN = 40
const pendingTunnelActions = reactive(new Map())
let tunnelActionRevision = 0
let stateSyncTimer = null
let trafficSyncTimer = null
let stateSyncInFlight = false
const trafficMonitorEnabled = ref(true)
const traffic = ref({ upBps: 0, downBps: 0 })
const trafficHistoryUp = ref([])
const trafficHistoryDown = ref([])

const logs = ref([{ id: 1, level: 'info', time: nowLabel(), message: 'Config storage mode: TOML' }])

const showJumperModal = ref(false)
const showTunnelModal = ref(false)
const showTunnelGroupModal = ref(false)
const showImportJumperModal = ref(false)
const showImportTunnelModal = ref(false)
const importJumperLoading = ref(false)
const importJumperError = ref('')
const importTunnelError = ref('')
const importJumperHasLoaded = ref(false)
const sshConfigSources = ref([])
const selectedImportJumperSourcePath = ref('')
const sshConfigCandidates = ref([])
const editingJumperId = ref(null)
const editingTunnelId = ref(null)
const editingTunnelStatus = computed(() => tunnels.value.find((item) => item.id === editingTunnelId.value)?.status || '')
const showJumperBasic = ref(true)
const showJumperAdvanced = ref(false)

const jumperForm = reactive(defaultJumperForm())
const tunnelForm = reactive(defaultTunnelForm())
const inlineJumperForm = reactive(defaultInlineJumperForm())

const jumperValidationError = ref('')
const inlineJumperValidationError = ref('')
const tunnelValidationError = ref('')
const tunnelGroupModalError = ref('')
const pendingTunnelGroupEditId = ref(null)
const actionDialog = reactive({
  visible: false,
  mode: 'alert',
  kind: '',
  message: '',
  confirmButtonClass: 'btn-primary',
  confirmLabel: '',
  onConfirm: null,
  secondaryLabel: '',
  secondaryButtonClass: 'btn-outline-primary',
  onSecondary: null,
  busy: false,
  error: '',
  preview: null,
  previewLoading: false,
  cleanup: true,
  operationId: '',
  lastResult: null,
})
const pendingDeletions = ref([])
let licenseStateRevision = 0
const redeemDialog = reactive({
  visible: false,
  initialTab: 'purchase',
  code: '',
  error: '',
  submitting: false,
})
const jumperTest = reactive({
  status: 'idle',
  message: '',
  debuggable: false,
})
const tunnelTest = reactive({
  status: 'idle',
  message: '',
  debuggable: false,
})
const jumperAiDebug = reactive(defaultAIDebugState())
const tunnelAiDebug = reactive(defaultAIDebugState())
const tunnelErrorAiDebugStates = reactive({})
const selectedAIDebugTunnel = ref(null)

const JUMPER_LIMITS = {
  name: 20,
  user: 50,
  host: 255,
  keyPath: 260,
  agentSocketPath: 512,
  password: 128,
  notes: 300,
  keepAliveIntervalMin: 0,
  keepAliveIntervalMax: 120000,
  timeoutMin: 100,
  timeoutMax: 120000,
}
const TUNNEL_LIMITS = {
  name: 20,
}
const FREE_PLAN_RUNNING_LIMIT = 3

const currentPage = computed(() => pages.value.find((page) => page.key === activePage.value))
const totalTunnels = computed(() => tunnels.value.length)
const runningTunnels = computed(() => tunnels.value.filter((tunnel) => ['running', 'reconnecting'].includes(tunnel.status)))
const stoppedTunnels = computed(() =>
  tunnels.value.filter((tunnel) => tunnel.status === 'stopped' || tunnel.status === 'error'),
)
const autoStartTunnels = computed(() => tunnels.value.filter((tunnel) => tunnel.autoStart))
const filteredLogs = computed(() => {
  if (selectedLogLevel.value === 'all') return logs.value
  return logs.value.filter((log) => log.level === selectedLogLevel.value)
})
const selectedAIDebugTunnelState = computed(() => {
  if (!selectedAIDebugTunnel.value?.id) return defaultAIDebugState()
  return ensureTunnelErrorAIDebugState(selectedAIDebugTunnel.value.id)
})
const selectedAIDebugTunnelTitle = computed(() => {
  const name = selectedAIDebugTunnel.value?.name || t('app.aiDebug.savedTunnelFallback')
  return t('app.aiDebug.modalTitle', { name })
})
const selectedAIDebugTunnelSubtitle = computed(() => {
  if (!selectedAIDebugTunnel.value) return ''
  return getTunnelJumperLabel(selectedAIDebugTunnel.value)
})

function jumperMatchesQuery(jumper, query) {
  const q = String(query || '').trim().toLowerCase()
  if (!q) return true
  const host = String(jumper.host || '')
  const port = String(jumper.port ?? '')
  const address = host.includes(':') && !host.startsWith('[') ? `[${host}]:${port}` : `${host}:${port}`
  return (
    String(jumper.name || '').toLowerCase().includes(q) ||
    host.toLowerCase().includes(q) ||
    String(jumper.user || '').toLowerCase().includes(q) ||
    address.toLowerCase().includes(q) ||
    (jumper.notes && String(jumper.notes).toLowerCase().includes(q))
  )
}

const filteredJumpers = computed(() => {
  const query = jumperSearchQuery.value.trim().toLowerCase()
  if (!query) return jumpers.value
  return jumpers.value.filter((jumper) => jumperMatchesQuery(jumper, query))
})

const filteredTunnels = computed(() => {
  const query = tunnelSearchQuery.value.trim().toLowerCase()
  if (!query) return tunnels.value

  return tunnels.value.filter((tunnel) => {
    const jumperName = getTunnelJumperLabel(tunnel).toLowerCase()
    return (
      String(tunnel.name || '').toLowerCase().includes(query) ||
      String(tunnel.localHost || '').toLowerCase().includes(query) ||
      String(tunnel.remoteHost || '').toLowerCase().includes(query) ||
      String(tunnel.publicUrl || '').toLowerCase().includes(query) ||
      jumperName.includes(query) ||
      (tunnel.description && tunnel.description.toLowerCase().includes(query))
    )
  })
})

const jumperNeedsPassword = computed(() => authNeedsPassword(jumperForm.authType))
const jumperShowsPassword = computed(() => authShowsPassword(jumperForm.authType))
const jumperNeedsKeyFile = computed(() => authNeedsKeyFile(jumperForm.authType))
const inlineJumperNeedsPassword = computed(() => authNeedsPassword(inlineJumperForm.authType))
const inlineJumperShowsPassword = computed(() => authShowsPassword(inlineJumperForm.authType))
const inlineJumperNeedsKeyFile = computed(() => authNeedsKeyFile(inlineJumperForm.authType))
const isPro = computed(() => proLicense.isPro)
const proExpiryLabel = computed(() => {
  if (!proLicense.isPro) return '--'
  if (proLicense.isLifetime) return t('config.lifetime')
  return formatDateTime(proLicense.expiresAt)
})

function defaultJumperForm() {
  return {
    name: '',
    host: '',
    port: 22,
    user: '',
    authType: 'ssh_key',
    keyPath: '',
    agentSocketPath: '',
    password: '',
    bypassHostVerification: true,
    keepAliveIntervalMs: 5000,
    timeoutMs: 5000,
    notes: '',
  }
}

function defaultTunnelForm() {
  return {
    name: '',
    provider: 'ssh',
    frpServerId: '',
    frpProxyName: '',
    frpRemotePort: 13000,
    frpDomain: '',
    groupId: 0,
    mode: 'local',
    jumperIds: [],
    nextJumperId: '',
    appendNewJumper: false,
    localHost: '127.0.0.1',
    localPort: 10022,
    remoteHost: '',
    remotePort: 22,
    access: 'private',
    expirationAmount: 30,
    expirationUnit: 'd',
    region: '',
    tunnelId: '',
    tunnelIdMode: 'auto',
    devTunnelName: '',
    originalTunnelId: '',
    tunnelName: '',
    hostname: '',
    portLocked: false,
    tunnelPort: 0,
    resourceError: '',
    resourceNote: '',
    remoteExpiration: '',
    publicUrl: '',
    autoStart: false,
    description: '',
  }
}

function splitDevTunnelExpiration(value) {
  const match = String(value || '').trim().toLowerCase().match(/^(\d+)([hd])$/)
  if (!match) return { amount: 30, unit: 'd' }
  return { amount: Number(match[1]), unit: match[2] }
}

function devTunnelExpirationError(amount, unit) {
  const parsed = Number(amount)
  const max = unit === 'h' ? 720 : 30
  if ((unit !== 'h' && unit !== 'd') || !Number.isInteger(parsed) || parsed < 1 || parsed > max) {
    return t('app.modals.tunnel.expirationInvalid')
  }
  return ''
}

// Mirrors devtunnel.NormalizeTunnelName: 3-49 lowercase letters, digits, or
// hyphens, starting and ending with a letter or digit. Consecutive inner
// hyphens follow the current Microsoft contract.
function devTunnelNameTakenRemotely(name) {
  return (devTunnelResources.items || []).some(item => sameTunnelID(item.id, name))
}

function devTunnelNameError(raw, { touched = true } = {}) {
  const name = normalizeDevTunnelName(raw)
  if (!name) {
    return touched ? t('app.modals.tunnel.customIdRequired') : ''
  }
  if (!DEV_TUNNEL_NAME_PATTERN.test(name)) {
    return t('app.modals.tunnel.customIdInvalid')
  }
  return ''
}

function defaultInlineJumperForm() {
  return {
    name: '',
    host: '',
    port: 22,
    user: '',
    authType: 'ssh_key',
    keyPath: '',
    agentSocketPath: '',
    password: '',
    bypassHostVerification: true,
    keepAliveIntervalMs: 5000,
    timeoutMs: 5000,
    notes: '',
  }
}

function defaultAIDebugState() {
  return {
    status: 'idle',
    error: '',
    result: null,
  }
}

function nowLabel() {
  return new Date().toLocaleString()
}

function formatDateTime(value) {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return String(value)
  return date.toLocaleDateString()
}

function applyLicenseState({ active = false, expire_time = null, is_lifetime = false, code = '' }) {
  proLicense.isPro = !!active
  proLicense.expiresAt = expire_time ? String(expire_time) : ''
  proLicense.isLifetime = !!is_lifetime
  proLicense.code = code ? String(code) : ''
}

function openExternalUrl(url) {
  if (!url) return
  try {
    BrowserOpenURL(url)
  } catch (_) {
    if (typeof window !== 'undefined') {
      window.open(url, '_blank', 'noopener,noreferrer')
    }
  }
}

function toText(value) {
  return String(value ?? '').trim()
}

function clipText(value, limit = AI_REPORT_MAX_FIELD_LEN) {
  const text = toText(value)
  if (limit <= 0 || text.length <= limit) return text
  return `${text.slice(0, limit)}...`
}



function nameUnits(text) {
  let units = 0
  for (const char of text || '') {
    units += /[\u3400-\u9fff\uf900-\ufaff]/.test(char) ? 2 : 1
  }
  return units
}

function isTunnelGroupNameTaken(name, excludeId = null) {
  const normalized = String(name || '')
    .trim()
    .toLowerCase()
  if (!normalized) return false
  return tunnelGroups.value.some((group) => {
    if (excludeId != null && Number(group.id) === Number(excludeId)) return false
    return (
      String(group.name || '')
        .trim()
        .toLowerCase() === normalized
    )
  })
}

function nextId(items) {
  return items.reduce((max, item) => Math.max(max, item.id), 0) + 1
}

function patchTunnelLocal(id, patch) {
  const index = tunnels.value.findIndex((item) => item.id === id)
  if (index === -1) return
  tunnels.value[index] = { ...tunnels.value[index], ...patch }
}

function formatLatencyLabel(latencyMs) {
  const ms = Number(latencyMs)
  if (!Number.isFinite(ms) || ms <= 0) return '--'
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(ms < 10000 ? 2 : 1)}s`
  if (ms < 3600000) return `${(ms / 60000).toFixed(ms < 600000 ? 2 : 1)}m`
  return `${(ms / 3600000).toFixed(2)}h`
}

function authNeedsPassword(authType) {
  return authType === 'password'
}

function authNeedsKeyFile(authType) {
  return authType === 'ssh_key'
}

function authShowsPassword(authType) {
  return authType === 'password' || authType === 'ssh_key'
}

function getAuthLabel(authType) {
  const matched = authOptions.value.find((item) => item.value === authType)
  return matched ? matched.label : t('app.options.auth.unknown')
}

function getJumperName(jumperId) {
  const jumper = jumpers.value.find((item) => item.id === jumperId)
  return jumper ? jumper.name : t('app.options.jumper.unknown')
}

function normalizeJumperIdList(ids) {
  const seen = new Set()
  return (Array.isArray(ids) ? ids : [])
    .map((id) => Number(id))
    .filter((id) => Number.isInteger(id) && id > 0)
    .filter((id) => {
      if (seen.has(id)) return false
      seen.add(id)
      return true
    })
}

function toHostKey(host) {
  return String(host || '')
    .trim()
    .toLowerCase()
}

function tunnelProvider(tunnelLike) {
  const provider = String(tunnelLike?.provider || 'ssh').trim().toLowerCase()
  return provider || 'ssh'
}

function getTunnelImportSignature(tunnelLike) {
  const provider = tunnelProvider(tunnelLike)
  if (provider === 'frp') return frpImportSignature(tunnelLike)
  const mode = String(tunnelLike?.mode || 'local')
    .trim()
    .toLowerCase()
  const localHost = toHostKey(tunnelLike?.localHost || tunnelLike?.devTunnel?.localHost || '127.0.0.1')
  const localPort = Number(tunnelLike?.localPort || tunnelLike?.devTunnel?.localPort) || 0
  const remoteHost = provider !== 'ssh' || mode === 'dynamic' ? '' : toHostKey(tunnelLike?.remoteHost)
  const remotePort = provider !== 'ssh' || mode === 'dynamic' ? 0 : Number(tunnelLike?.remotePort) || 0
  const jumperKey = provider === 'ssh' ? normalizeJumperIdList(tunnelLike?.jumperIds).join(',') : ''
  const access = provider === 'devtunnel' ? String(tunnelLike?.devTunnel?.access || tunnelLike?.access || 'private') : ''
  return `${provider}|${mode}|${localHost}|${localPort}|${remoteHost}|${remotePort}|${jumperKey}|${access}`
}

function getNextTunnelJumperCandidate(selectedIds = []) {
  if (!jumpers.value.length) return ''
  const selected = new Set(normalizeJumperIdList(selectedIds))
  const candidate = jumpers.value.find((item) => !selected.has(item.id))
  return candidate ? candidate.id : jumpers.value[0].id
}

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

function describeDevTunnel(tunnel) {
  const id = String(tunnel?.devTunnel?.tunnelId || '').trim()
  const port = tunnel?.devTunnel?.tunnelPort || tunnel?.devTunnel?.localPort || tunnel?.localPort
  const live = preferDevTunnelURL(devTunnelPortURLs(tunnel?.publicUrl, port).join(','))
  const waiting = tunnel?.status === 'running' || tunnel?.status === 'busy'
  const pendingURL = t(waiting ? 'app.tunnels.waitingForUrl' : 'app.tunnels.publicUrlAfterStart')
  const state = devTunnelResources.state
  const resource = state === 'ready' && id
    ? (devTunnelResources.items || []).find((item) => sameDevTunnelID(item.id, id))
    : null
  const remoteURL = preferDevTunnelURL(devTunnelPortURLs(resource?.urls || [], port).join(','))
  const url = live || remoteURL
  let routeTitle = url || pendingURL
  if (id && state === 'ready' && !resource) {
    routeTitle = t('app.tunnels.connection.remoteMissing')
  } else if (['auth', 'network', 'error', 'unavailable', 'permission'].includes(state)) {
    routeTitle = t(url ? 'app.tunnels.connection.unverifiedHint' : 'app.tunnels.connection.remoteUnverified')
  }
  return {
    connection: id || t('app.tunnels.connection.pendingCreate'),
    connectionTitle: id,
    routeBottom: url || pendingURL,
    routeTitle,
    url,
  }
}

function describeCloudflareTunnel(tunnel) {
  const id = String(tunnel?.cloudflare?.tunnelId || '').trim()
  const url = cloudflarePublicURL(tunnel?.cloudflare?.hostname)
  return {
    connection: id || t('app.tunnels.connection.pendingCreate'),
    connectionTitle: id,
    routeBottom: url,
    routeTitle: url,
    url,
  }
}

function getTunnelJumperLabel(tunnel) {
  switch (tunnelProvider(tunnel)) {
    case 'devtunnel':
      return describeDevTunnel(tunnel).connection
    case 'frp':
      return frpState.servers.find(s => s.id === tunnel.frp?.serverId)?.name || t('app.frp.missingServer')
    case 'cloudflare':
      return describeCloudflareTunnel(tunnel).connection
    default:
      break
  }
  const ids = normalizeJumperIdList(tunnel?.jumperIds)
  if (!ids.length) return t('app.options.jumper.unknown')
  const names = ids.map((id) => getJumperName(id))
  return names.join(' -> ')
}

function preferDevTunnelURL(raw) {
  const parts = String(raw || '')
    .split(',')
    .map((part) => part.trim().replace(/^["']|["']$/g, ''))
    .filter((part) => part.startsWith('https://') || part.startsWith('http://'))
  const preferred = parts.find((part) => {
    try {
      const parsed = new URL(part)
      return parsed.protocol === 'https:' && (parsed.port === '' || parsed.port === '443')
    } catch {
      return false
    }
  })
  return preferred || parts[0] || ''
}

function normalizeTunnelFromBackend(tunnel) {
  const rawLatency = Number(tunnel?.latencyMs)
  const rawGroupId = Number(tunnel?.groupId)
  return {
    ...tunnel,
    provider: tunnelProvider(tunnel),
    devTunnel: tunnel?.devTunnel || null,
    publicUrl: String(tunnel?.publicUrl || '').trim(),
    groupId: Number.isInteger(rawGroupId) && rawGroupId > 0 ? rawGroupId : 0,
    jumperIds: normalizeJumperIdList(tunnel?.jumperIds),
    latencyMs: Number.isFinite(rawLatency) && rawLatency > 0 ? rawLatency : 0,
  }
}

function logEvent(level, message) {
  logs.value.unshift({
    id: nextId(logs.value),
    level,
    time: nowLabel(),
    message,
  })
}

function errorMessage(err, fallback = 'Operation failed.') {
  if (!err) return fallback
  const raw = typeof err === 'string' ? err : (typeof err.message === 'string' && err.message ? err.message : '')
  if (!raw) return fallback
  if (isDevTunnelNameConflict(raw)) return t('app.modals.tunnel.customIdRemoteConflict')
  return raw
}

function aiDebugErrorMessage(err, fallback = 'AI Debug failed.') {
  const message = errorMessage(err, fallback)
  if (/quota exceeded|daily quota/i.test(message)) {
    return t('app.aiDebug.quotaExceeded', { freeLimit: 10, proLimit: 200 })
  }
  if (/AI_PROVIDER_BUSY|rate limited|rate_limit_exceeded/i.test(message)) {
    return t('app.aiDebug.providerBusy')
  }
  if (
    /HTTP 5\d\d|cannot connect|connection refused|timeout|not configured|provider request failed|backend/i.test(message)
  ) {
    return t('app.aiDebug.unavailable')
  }
  return message
}

function buildAIDebugReportBody({ targetType, result }) {
  const lines = [
    'Please describe why this AI output is inappropriate:',
    '',
    '[Your report]',
    '',
    '--- Context (auto-filled) ---',
    'Feature: AI Debug',
    `Target Type: ${clipText(targetType || 'unknown', 40)}`,
    `App Version: ${clipText(appMeta.version, 64)}`,
    `OS: ${clipText(typeof navigator !== 'undefined' ? navigator.platform || 'unknown' : 'unknown', 80)}`,
    `UI Locale: ${clipText(locale.value || 'en', 24)}`,
    `Timestamp (UTC): ${new Date().toISOString()}`,
    '',
    '[AI Output]',
    `Reason: ${clipText(result?.reason)}`,
    `Summary: ${clipText(result?.summary)}`,
    `Steps: ${clipText(Array.isArray(result?.steps) ? result.steps.join(' | ') : '')}`,
  ]
  return lines.join('\n').trim()
}

async function showAIDebugReportFallbackDialog(reportBody) {
  openActionDialog({
    mode: 'alert',
    message: t('app.aiDebug.reportOpenFailed'),
    confirmButtonClass: 'btn-primary',
    confirmLabel: t('app.aiDebug.copyReportBody'),
    onConfirm: async () => {
      const copied = await writeTextToClipboard(reportBody)
      setConfigMessage(copied ? t('app.aiDebug.copyBodyDone') : t('app.aiDebug.copyFailed'))
    },
    secondaryLabel: t('app.aiDebug.copySupportEmail'),
    secondaryButtonClass: 'btn-outline-secondary',
    onSecondary: async () => {
      const copied = await writeTextToClipboard(AI_REPORT_SUPPORT_EMAIL)
      setConfigMessage(copied ? t('app.aiDebug.copyEmailDone') : t('app.aiDebug.copyFailed'))
    },
  })
}

async function reportAIDebugContent(targetType, state) {
  const result = state?.result
  if (!result) return

  const body = buildAIDebugReportBody({ targetType, result })
  try {
    const openResult = await OpenReportEmail({
      subject: AI_REPORT_SUBJECT,
      body,
    })
    if (!openResult?.success) {
      await showAIDebugReportFallbackDialog(body)
      return
    }
    setConfigMessage(t('app.aiDebug.reportDraftOpened'))
  } catch (_) {
    await showAIDebugReportFallbackDialog(body)
  }
}

async function loadStateFromBackend(options = {}) {
  const { silent = false } = options
  try {
    const revisionAtFetch = tunnelActionRevision
    const state = await GetState()
    jumpers.value = Array.isArray(state?.jumpers) ? state.jumpers : []
    tunnelGroups.value = Array.isArray(state?.groups) ? state.groups : []
    activeSlots.value = Number(state?.activeSlots) || 0
    const backendTunnels = (Array.isArray(state?.tunnels) ? state.tunnels : []).map(normalizeTunnelFromBackend)
    const validTunnelIds = new Set(backendTunnels.map((item) => String(item.id)))
    Object.keys(tunnelErrorAiDebugStates).forEach((key) => {
      if (!validTunnelIds.has(key)) {
        delete tunnelErrorAiDebugStates[key]
      }
    })
    backendTunnels.forEach((item) => {
      if (item.status !== 'error' || !item.lastError) {
        delete tunnelErrorAiDebugStates[String(item.id)]
      }
    })

    // A completed toggle makes an earlier poll stale.
    if (revisionAtFetch !== tunnelActionRevision) return
    const localTunnels = new Map(tunnels.value.map((item) => [item.id, item]))
    tunnels.value = backendTunnels.map((item) => {
      if (pendingTunnelActions.has(item.id)) {
        return localTunnels.get(item.id) || item
      }
      return item
    })
  } catch (err) {
    const message = errorMessage(err, 'Failed to load config from backend.')
    if (silent) return
    configMessage.value = message
    logEvent('error', message)
  } finally {
    initialStateLoading.value = false
  }
}

function syncStateSilently() {
  if (stateSyncInFlight) return
  stateSyncInFlight = true
  loadStateFromBackend({ silent: true }).finally(() => {
    stateSyncInFlight = false
  })
}

function pushTrafficHistory(target, value) {
  const next = Math.max(0, Number(value) || 0)
  target.value = [...target.value, next].slice(-TRAFFIC_HISTORY_LEN)
}

async function syncTrafficSilently() {
  if (!trafficMonitorEnabled.value) return
  try {
    const stats = await GetTrafficStats()
    const upBps = Number(stats?.upBps) || 0
    const downBps = Number(stats?.downBps) || 0
    traffic.value = { upBps, downBps }
    pushTrafficHistory(trafficHistoryUp, upBps)
    pushTrafficHistory(trafficHistoryDown, downBps)
  } catch (_) {
    // best-effort background sync
  }
}

function stopTrafficSync() {
  if (trafficSyncTimer !== null) {
    window.clearInterval(trafficSyncTimer)
    trafficSyncTimer = null
  }
  traffic.value = { upBps: 0, downBps: 0 }
  trafficHistoryUp.value = []
  trafficHistoryDown.value = []
}

function startTrafficSync() {
  if (!trafficMonitorEnabled.value || trafficSyncTimer !== null) return
  void syncTrafficSilently()
  trafficSyncTimer = window.setInterval(syncTrafficSilently, TRAFFIC_SYNC_INTERVAL_MS)
}

function onTrafficMonitorChange(enabled) {
  trafficMonitorEnabled.value = !!enabled
  if (trafficMonitorEnabled.value) {
    startTrafficSync()
  } else {
    stopTrafficSync()
  }
}

function switchPage(pageKey) {
  activePage.value = pageKey
  if (pageKey === 'providers') {
    void refreshDevTunnelStatus(false)
    if (providerTab.value === 'cloudflare') {
      void refreshCloudflareStatus(false)
      void loadCloudflareResources(false)
    }
  }
  if (pageKey === 'providers' || pageKey === 'tunnels' || pageKey === 'overview') void loadDevTunnelResources(false)
}

function openTunnelProvider(provider) {
  providerTab.value = ['devtunnel', 'cloudflare', 'frp'].includes(provider) ? provider : 'ssh'
  switchPage('providers')
}

let devTunnelCheckTimer = 0

async function checkDevTunnel() {
  if (devTunnelBusy.value && devTunnelBusy.value !== 'checked') return
  if (devTunnelCheckTimer) {
    window.clearTimeout(devTunnelCheckTimer)
    devTunnelCheckTimer = 0
  }
  devTunnelBusy.value = 'check'
  try {
    await refreshDevTunnelStatus()
  } finally {
    devTunnelBusy.value = 'checked'
    devTunnelCheckTimer = window.setTimeout(() => {
      devTunnelCheckTimer = 0
      if (devTunnelBusy.value === 'checked') devTunnelBusy.value = ''
    }, 1200)
  }
}

async function refreshDevTunnelStatus(force = true) {
  if (devTunnelStatusRequest) return devTunnelStatusRequest
  if (!force && Date.now() - devTunnelStatusAt < 5 * 60 * 1000) return
  const request = fetchDevTunnelStatus()
  devTunnelStatusRequest = request
  try { await request } finally {
    if (devTunnelStatusRequest === request) devTunnelStatusRequest = null
  }
}

async function fetchDevTunnelStatus() {
  devTunnelStatus.value = { ...devTunnelStatus.value, checking: true }
  try {
    devTunnelStatus.value = await GetDevTunnelStatus()
    devTunnelStatusAt = Date.now()
  } catch (err) {
    devTunnelStatus.value = {
      ...(devTunnelStatus.value || {}),
      accountState: devTunnelStatus.value?.accountState || 'unknown',
      detail: errorMessage(err),
    }
  } finally {
    devTunnelStatus.value = { ...devTunnelStatus.value, checking: false }
  }
}

async function loadDevTunnelResources(force = false) {
  if (devTunnelListRequest) {
    await devTunnelListRequest
    if (!force) return
  }
  if (!force && devTunnelResources.state === 'ready' && Date.now() - devTunnelResourceCacheAt < devTunnelCacheTTL) return
  if (force) devTunnelDetailCache.clear()
  const request = fetchDevTunnelResources(force)
  devTunnelListRequest = request
  try { await request } finally {
    if (devTunnelListRequest === request) devTunnelListRequest = null
  }
}

async function fetchDevTunnelResources(force) {
  const seq = ++devTunnelResourceSeq
  if (!devTunnelResources.items.length) devTunnelResources.state = 'loading'
  if (force) devTunnelResourceBusy.value = 'refresh'
  try {
    const result = await ListDevTunnelResources(force)
    if (seq !== devTunnelResourceSeq) return
    if (result?.state === 'ready') {
      devTunnelResources.state = 'ready'
      devTunnelResources.items = result.items || []
      for (const item of devTunnelResources.items) {
        if (!item.portsKnown) continue
        devTunnelDetailCache.set(item.id, { result: { state: 'ready', resource: item }, at: Date.now() })
      }
      devTunnelResources.detail = ''
      devTunnelResourceCacheAt = Date.now()
    } else {
      devTunnelResources.state = result?.state || 'error'
      devTunnelResources.detail = result?.detail || ''
    }
  } catch (err) {
    if (seq !== devTunnelResourceSeq) return
    devTunnelResources.state = 'error'
    devTunnelResources.detail = errorMessage(err)
  } finally {
    if (seq === devTunnelResourceSeq && devTunnelResourceBusy.value === 'refresh') devTunnelResourceBusy.value = ''
  }
}

function applyRemoteExpiration(value) {
  const match = String(value || '').trim().toLowerCase().match(/^(\d+)([hd])$/)
  if (!match) {
    tunnelForm.remoteExpiration = String(value || '').trim()
    return
  }
  tunnelForm.remoteExpiration = ''
  tunnelForm.expirationAmount = Number(match[1])
  tunnelForm.expirationUnit = match[2]
}

async function inspectDevTunnelID(id) {
  const seq = ++devTunnelInspectSeq
  const trimmed = String(id || '').trim()
  tunnelForm.resourceError = ''
  tunnelForm.portLocked = false
  if (trimmed === '__custom__') {
    tunnelForm.tunnelIdMode = 'custom'
    tunnelForm.tunnelId = ''
    tunnelForm.tunnelPort = 0
    tunnelForm.publicUrl = ''
    tunnelForm.remoteExpiration = ''
    tunnelForm.resourceNote = ''
    return
  }
  if (!trimmed) {
    tunnelForm.tunnelIdMode = 'auto'
    tunnelForm.tunnelId = ''
    tunnelForm.devTunnelName = ''
    tunnelForm.access = 'private'
    tunnelForm.remoteExpiration = ''
    tunnelForm.publicUrl = ''
    tunnelForm.resourceNote = t('app.modals.tunnel.autoCreateHint')
    return
  }
  if (tunnelForm.tunnelId !== trimmed) { tunnelForm.publicUrl = ''; tunnelForm.tunnelPort = 0 }
  tunnelForm.tunnelIdMode = 'bind'
  tunnelForm.tunnelId = trimmed
  tunnelForm.access = 'unknown'
  tunnelForm.resourceNote = t('app.modals.tunnel.tunnelIdLoading')
  try {
    const detail = await getDevTunnelResourceCached(trimmed)
    if (seq !== devTunnelInspectSeq || tunnelForm.tunnelId !== trimmed) return
    if (detail?.state === 'not_found') {
      tunnelForm.resourceError = t('app.tunnels.connection.remoteMissing')
      tunnelForm.resourceNote = ''
      return
    }
    if (detail?.state !== 'ready') {
      tunnelForm.resourceNote = detail?.detail || t('app.tunnels.connection.remoteUnverified')
      return
    }
    const resource = detail.resource || {}
    const ports = resource.ports || []
    tunnelForm.publicUrl = devTunnelPortURLs([
      ...String(tunnelForm.publicUrl || '').split(','),
      ...(resource.urls || []),
    ], tunnelForm.tunnelPort).join(', ')
    tunnelForm.access = resource.access === 'public' || resource.access === 'private' ? resource.access : 'unknown'
    applyRemoteExpiration(resource.expiration)
    tunnelForm.portLocked = false
    tunnelForm.resourceError = ''
    tunnelForm.resourceNote = ''
  } catch (err) {
    if (seq !== devTunnelInspectSeq) return
    tunnelForm.resourceError = errorMessage(err)
    tunnelForm.resourceNote = ''
  }
}

let devTunnelRegionSeq = 0
async function loadDevTunnelRegions(measure = false) {
  if (!measure && devTunnelRegionProbe.regions.length) return
  const seq = ++devTunnelRegionSeq
  if (measure) devTunnelRegionProbe.loading = true
  try {
    const result = await LoadDevTunnelRegions(!!measure, !!measure)
    if (seq !== devTunnelRegionSeq) return
    devTunnelRegionProbe.state = result?.state || 'idle'
    devTunnelRegionProbe.detail = result?.detail || ''
    devTunnelRegionProbe.regions = result?.regions || []
    devTunnelRegionProbe.measuredAt = result?.measuredAt || ''
    devTunnelRegionProbe.cached = !!result?.cached
  } catch (err) {
    if (seq !== devTunnelRegionSeq) return
    devTunnelRegionProbe.state = 'error'
    devTunnelRegionProbe.detail = errorMessage(err)
  } finally {
    if (seq === devTunnelRegionSeq) devTunnelRegionProbe.loading = false
  }
}

async function createDevTunnelResource(input) {
  devTunnelResourceBusy.value = 'create'
  devTunnelCreated.value = null
  devTunnelCreateError.value = ''
  try {
    const detail = await CreateDevTunnelResource(input)
    if (detail?.state !== 'ready' || !detail.resource?.id) {
      devTunnelCreateError.value = devTunnelCreateFailureText(detail?.detail)
      return
    }
    // Creation already returns the confirmed resource; don't wait for a full CLI list.
    // Invalidate older list requests so they cannot remove the newly created option.
    ++devTunnelResourceSeq
    devTunnelListRequest = null
    devTunnelDetailCache.set(detail.resource.id, { result: detail, at: Date.now() })
    devTunnelResources.items = [
      detail.resource,
      ...devTunnelResources.items.filter((item) => item.id !== detail.resource.id),
    ]
    devTunnelResources.state = 'ready'
    devTunnelResources.detail = ''
    devTunnelResourceCacheAt = Date.now()
    devTunnelCreated.value = detail.resource
  } catch (err) {
    devTunnelCreateError.value = regionCreateMessage(errorMessage(err), t)
  } finally {
    devTunnelResourceBusy.value = ''
  }
}

function devTunnelCreateFailureText(detail) {
  if (isDevTunnelNameConflict(detail)) return t('app.modals.tunnel.customIdRemoteConflict')
  return regionCreateMessage(detail, t) || t('app.providers.loadFailed')
}

async function deleteDevTunnelResourceByID(payload) {
  const id = typeof payload === 'string' ? payload : payload?.id
  if (!id) return
  devTunnelResourceBusy.value = 'delete'
  try {
    const result = await DeleteProviderResourceWithOptions('devtunnel', id, false)
    await applyProviderDeleteResult('devtunnel', id, result)
  } catch (err) {
    devTunnelActionResult.value = { state: 'error', detail: errorMessage(err), token: Date.now() }
  } finally {
    devTunnelResourceBusy.value = ''
  }
}

async function applyProviderDeleteResult(provider, id, result) {
  const completed = result?.status === 'completed' || result?.state === 'deleted'
  const target = provider === 'cloudflare' ? cloudflareActionResult : devTunnelActionResult
  if (completed) {
    target.value = { state: 'deleted', token: Date.now() }
    if (provider === 'cloudflare') {
      cloudflareDetailCache.delete(id)
      ++cloudflareResourceSeq
      cloudflareListRequest = null
      cloudflareResourceCacheAt = 0
      await loadCloudflareResources(false)
    } else {
      devTunnelDetailCache.delete(id)
      ++devTunnelResourceSeq
      devTunnelListRequest = null
      devTunnelResourceCacheAt = 0
      await loadDevTunnelResources(false)
    }
    await refreshPendingDeletions()
    return
  }
  if (result?.detail) logEvent('error', result.detail)
  target.value = {
    state: 'error',
    detail: deletionText(result),
    operationId: result?.operationId || '',
    token: Date.now(),
  }
  await refreshPendingDeletions()
}

async function retryProviderCleanup(operationID, provider) {
  const busy = provider === 'cloudflare' ? cloudflareResourceBusy : devTunnelResourceBusy
  busy.value = 'delete'
  try {
    const result = await RetryDeletionCleanup(operationID)
    await applyProviderDeleteResult(provider, '', result)
  } catch (err) {
    const target = provider === 'cloudflare' ? cloudflareActionResult : devTunnelActionResult
    target.value = { state: 'error', detail: errorMessage(err), operationId: operationID, token: Date.now() }
  } finally {
    busy.value = ''
  }
}

function getDevTunnelResourceCached(id) {
  const cached = devTunnelDetailCache.get(id)
  if (cached?.request) return cached.request
  if (cached?.result && Date.now() - cached.at < devTunnelCacheTTL) return Promise.resolve(cached.result)
  const epoch = devTunnelCacheEpoch
  const entry = { ...cached }
  entry.request = GetDevTunnelResource(id, false).then((result) => {
    if (epoch === devTunnelCacheEpoch && devTunnelDetailCache.get(id) === entry && result?.state === 'ready') {
      entry.result = result
      entry.at = Date.now()
    }
    return result
  }).finally(() => { delete entry.request })
  devTunnelDetailCache.set(id, entry)
  return entry.request
}

async function inspectDevTunnelResource(id) {
  const seq = ++devTunnelDetailSeq
  const cached = devTunnelDetailCache.get(id)?.result
  devTunnelDetail.value = cached || { state: 'loading', resource: { id } }
  try {
    const result = await getDevTunnelResourceCached(id)
    if (seq === devTunnelDetailSeq) devTunnelDetail.value = result
  } catch (err) {
    if (seq === devTunnelDetailSeq && !cached) devTunnelDetail.value = { state: 'error', detail: errorMessage(err) }
  }
}

function openNewTunnelFromDevTunnelID(id) {
  openNewTunnel()
  tunnelForm.provider = 'devtunnel'
  tunnelForm.mode = 'auto'
  tunnelForm.localHost = '127.0.0.1'
  tunnelForm.localPort = 8080
  tunnelForm.jumperIds = []
  tunnelForm.appendNewJumper = false
  tunnelForm.originalTunnelId = ''
  void inspectDevTunnelID(id)
}

function manageDevTunnelIDs() {
  closeTunnelModal()
  openTunnelProvider('devtunnel')
}

function manageCloudflareIDs() {
  closeTunnelModal()
  openTunnelProvider('cloudflare')
}

let cloudflareCheckTimer = 0

async function checkCloudflare() {
  if (cloudflareBusy.value && cloudflareBusy.value !== 'checked') return
  if (cloudflareCheckTimer) {
    window.clearTimeout(cloudflareCheckTimer)
    cloudflareCheckTimer = 0
  }
  cloudflareBusy.value = 'check'
  try {
    await refreshCloudflareStatus()
  } finally {
    cloudflareBusy.value = 'checked'
    cloudflareCheckTimer = window.setTimeout(() => {
      cloudflareCheckTimer = 0
      if (cloudflareBusy.value === 'checked') cloudflareBusy.value = ''
    }, 1200)
  }
}

async function refreshCloudflareStatus(force = true) {
  if (cloudflareStatusRequest) return cloudflareStatusRequest
  if (!force && Date.now() - cloudflareStatusAt < 5 * 60 * 1000) return
  const request = fetchCloudflareStatus()
  cloudflareStatusRequest = request
  try { await request } finally {
    if (cloudflareStatusRequest === request) cloudflareStatusRequest = null
  }
}

async function fetchCloudflareStatus() {
  cloudflareStatus.value = { ...cloudflareStatus.value, checking: true }
  try {
    cloudflareStatus.value = await GetCloudflareStatus()
    cloudflareStatusAt = Date.now()
  } catch (err) {
    cloudflareStatus.value = {
      ...(cloudflareStatus.value || {}),
      accountState: cloudflareStatus.value?.accountState || 'unknown',
      detail: errorMessage(err),
    }
  } finally {
    cloudflareStatus.value = { ...cloudflareStatus.value, checking: false }
  }
  if (cloudflareStatus.value?.accountState === 'signed_in') void loadCloudflareZones(false)
}

async function loadCloudflareZones(force = false) {
  if (cloudflareZoneRequest) {
    await cloudflareZoneRequest
    if (!force) return
  }
  if (!force && cloudflareZones.state === 'ready' && Date.now() - cloudflareZoneCacheAt < cloudflareCacheTTL) return
  const request = fetchCloudflareZones(force)
  cloudflareZoneRequest = request
  try { await request } finally {
    if (cloudflareZoneRequest === request) cloudflareZoneRequest = null
  }
}

async function fetchCloudflareZones(force) {
  const seq = ++cloudflareZoneSeq
  cloudflareZones.state = 'loading'
  try {
    const result = await ListCloudflareZones(force)
    if (seq !== cloudflareZoneSeq) return
    cloudflareZones.state = result?.state || 'error'
    cloudflareZones.detail = result?.detail || ''
    cloudflareZones.items = result?.state === 'ready' ? (result.items || []) : []
    if (result?.state === 'ready') cloudflareZoneCacheAt = Date.now()
  } catch (err) {
    if (seq !== cloudflareZoneSeq) return
    cloudflareZones.state = 'error'
    cloudflareZones.detail = errorMessage(err)
    cloudflareZones.items = []
  }
}

async function loadCloudflareResources(force = false) {
  if (force) void loadCloudflareZones(true)
  if (cloudflareListRequest) {
    await cloudflareListRequest
    if (!force) return
  }
  if (!force && cloudflareResources.state === 'ready' && Date.now() - cloudflareResourceCacheAt < cloudflareCacheTTL) return
  if (force) cloudflareDetailCache.clear()
  const request = fetchCloudflareResources(force)
  cloudflareListRequest = request
  try { await request } finally {
    if (cloudflareListRequest === request) cloudflareListRequest = null
  }
}

async function fetchCloudflareResources(force) {
  const seq = ++cloudflareResourceSeq
  if (!cloudflareResources.items.length) cloudflareResources.state = 'loading'
  if (force) cloudflareResourceBusy.value = 'refresh'
  try {
    const result = await ListCloudflareResources(force)
    if (seq !== cloudflareResourceSeq) return
    if (result?.state === 'ready') {
      cloudflareResources.state = 'ready'
      cloudflareResources.items = result.items || []
      for (const item of cloudflareResources.items) {
        cloudflareDetailCache.set(item.id, { result: { state: 'ready', resource: item }, at: Date.now() })
      }
      cloudflareResources.detail = ''
      cloudflareResourceCacheAt = Date.now()
    } else {
      cloudflareResources.state = result?.state || 'error'
      cloudflareResources.detail = result?.detail || ''
    }
  } catch (err) {
    if (seq !== cloudflareResourceSeq) return
    cloudflareResources.state = 'error'
    cloudflareResources.detail = errorMessage(err)
  } finally {
    if (seq === cloudflareResourceSeq && cloudflareResourceBusy.value === 'refresh') cloudflareResourceBusy.value = ''
  }
}

function getCloudflareResourceCached(id) {
  const cached = cloudflareDetailCache.get(id)
  if (cached?.request) return cached.request
  if (cached?.result && Date.now() - cached.at < cloudflareCacheTTL) return Promise.resolve(cached.result)
  const epoch = cloudflareCacheEpoch
  const entry = { ...cached }
  entry.request = GetCloudflareResource(id, false).then((result) => {
    if (epoch === cloudflareCacheEpoch && cloudflareDetailCache.get(id) === entry && result?.state === 'ready') {
      entry.result = result
      entry.at = Date.now()
    }
    return result
  }).finally(() => { delete entry.request })
  cloudflareDetailCache.set(id, entry)
  return entry.request
}

async function inspectCloudflareResource(id) {
  const seq = ++cloudflareDetailSeq
  const cached = cloudflareDetailCache.get(id)?.result
  cloudflareDetail.value = cached || { state: 'loading', resource: { id } }
  try {
    const result = await getCloudflareResourceCached(id)
    if (seq === cloudflareDetailSeq) cloudflareDetail.value = result
  } catch (err) {
    if (seq === cloudflareDetailSeq && !cached) cloudflareDetail.value = { state: 'error', detail: errorMessage(err) }
  }
}

async function inspectCloudflareID(id) {
  const seq = ++cloudflareInspectSeq
  const trimmed = String(id || '').trim()
  tunnelForm.resourceError = ''
  if (!trimmed) {
    tunnelForm.tunnelIdMode = 'auto'
    tunnelForm.tunnelId = ''
    tunnelForm.resourceNote = t('app.cloudflare.autoCreate')
    return
  }
  tunnelForm.tunnelIdMode = 'bind'
  tunnelForm.tunnelId = trimmed
  tunnelForm.access = 'unknown'
  tunnelForm.resourceNote = t('app.modals.tunnel.tunnelIdLoading')
  const item = (cloudflareResources.items || []).find((entry) => String(entry.id).toLowerCase() === trimmed.toLowerCase())
  if (item?.name) tunnelForm.tunnelName = item.name
  if (item && item.credentialReady === false) {
    tunnelForm.resourceError = t('app.cloudflare.credentialsMissing')
    tunnelForm.resourceNote = ''
    return
  }
  try {
    const detail = await getCloudflareResourceCached(trimmed)
    if (seq !== cloudflareInspectSeq || tunnelForm.tunnelId !== trimmed) return
    if (detail?.state !== 'ready') {
      tunnelForm.resourceNote = detail?.detail || t('app.tunnels.connection.remoteUnverified')
      return
    }
    if (detail.resource?.name) tunnelForm.tunnelName = detail.resource.name
    if (detail.resource?.credentialReady === false) {
      tunnelForm.resourceError = t('app.cloudflare.credentialsMissing')
      tunnelForm.resourceNote = ''
      return
    }
    tunnelForm.resourceError = ''
    tunnelForm.resourceNote = ''
  } catch (err) {
    if (seq !== cloudflareInspectSeq) return
    tunnelForm.resourceError = errorMessage(err)
    tunnelForm.resourceNote = ''
  }
}

async function createCloudflareResource(input) {
  cloudflareResourceBusy.value = 'create'
  cloudflareCreated.value = null
  cloudflareCreateError.value = ''
  try {
    const detail = await CreateCloudflareResource(input)
    if (detail?.state !== 'ready' || !detail.resource?.id) {
      cloudflareCreateError.value = detail?.detail || t('app.providers.loadFailed')
      return
    }
    ++cloudflareResourceSeq
    cloudflareListRequest = null
    cloudflareDetailCache.set(detail.resource.id, { result: detail, at: Date.now() })
    cloudflareResources.items = [
      detail.resource,
      ...cloudflareResources.items.filter((item) => item.id !== detail.resource.id),
    ]
    cloudflareResources.state = 'ready'
    cloudflareResources.detail = ''
    cloudflareResourceCacheAt = Date.now()
    cloudflareCreated.value = detail.resource
  } catch (err) {
    cloudflareCreateError.value = errorMessage(err)
  } finally {
    cloudflareResourceBusy.value = ''
  }
}

async function deleteCloudflareResourceByID(payload) {
  const id = typeof payload === 'string' ? payload : payload?.id
  if (!id) return
  const cleanupDNS = typeof payload === 'string' ? false : !!payload?.cleanupDNS
  cloudflareResourceBusy.value = 'delete'
  try {
    const result = await DeleteProviderResourceWithOptions('cloudflare', id, cleanupDNS)
    await applyProviderDeleteResult('cloudflare', id, result)
  } catch (err) {
    cloudflareActionResult.value = { state: 'error', detail: errorMessage(err), token: Date.now() }
  } finally {
    cloudflareResourceBusy.value = ''
  }
}

function openNewTunnelFromCloudflareID(id) {
  openNewTunnel()
  tunnelForm.provider = 'cloudflare'
  tunnelForm.mode = 'http'
  tunnelForm.localHost = '127.0.0.1'
  tunnelForm.localPort = 3000
  tunnelForm.jumperIds = []
  tunnelForm.appendNewJumper = false
  tunnelForm.originalTunnelId = ''
  tunnelForm.hostname = ''
  void inspectCloudflareID(id)
  void loadCloudflareZones(false)
}

async function installCloudflare() {
  cloudflareBusy.value = 'install'
  try {
    await InstallCloudflareCLI()
  } catch (err) {
    await refreshCloudflareStatus()
    cloudflareStatus.value = { ...(cloudflareStatus.value || {}), detail: errorMessage(err) }
    return
  } finally {
    cloudflareBusy.value = ''
  }
  await refreshCloudflareStatus()
}

function cancelCloudflareInstall() {
  CancelCloudflareInstall()
  cloudflareBusy.value = ''
}

async function loginCloudflare(zoneID = '') {
  stopCloudflareLoginPoll()
  cloudflareBusy.value = 'login:cloudflare'
  cloudflareStatus.value = {
    ...(cloudflareStatus.value || {}),
    loginInProgress: true,
    loginPhase: 'starting',
    loginError: '',
    loginResult: '',
    loginUrl: '',
  }
  try {
    await startCloudflareZoneAuthorization(typeof zoneID === 'string' ? zoneID : '')
    startCloudflareLoginPoll()
  } catch (err) {
    cloudflareBusy.value = ''
    cloudflareStatus.value = {
      ...(cloudflareStatus.value || {}),
      loginInProgress: false,
      loginPhase: '',
      loginError: errorMessage(err),
    }
  }
}

function startCloudflareLoginPoll() {
  stopCloudflareLoginPoll()
  cloudflareLoginPoll = window.setInterval(() => { void pollCloudflareLogin() }, 2000)
}

function stopCloudflareLoginPoll() {
  if (cloudflareLoginPoll !== null) {
    window.clearInterval(cloudflareLoginPoll)
    cloudflareLoginPoll = null
  }
}

async function pollCloudflareLogin() {
  if (cloudflareLoginPollInFlight) return
  cloudflareLoginPollInFlight = true
  try { await finishCloudflareLogin() } finally { cloudflareLoginPollInFlight = false }
}

async function finishCloudflareLogin() {
  await refreshCloudflareStatus()
  const status = cloudflareStatus.value || {}
  if (!status.loginInProgress) {
    cloudflareBusy.value = ''
    stopCloudflareLoginPoll()
    if (status.accountState === 'signed_in') await cloudflareAuthorizationsChanged()
  }
}

async function cloudflareAuthorizationsChanged() {
  ++cloudflareCacheEpoch
  ++cloudflareResourceSeq
  ++cloudflareDetailSeq
  ++cloudflareZoneSeq
  cloudflareListRequest = null
  cloudflareZoneRequest = null
  cloudflareDetailCache.clear()
  cloudflareDetail.value = null
  cloudflareZoneCacheAt = 0
  cloudflareResourceCacheAt = 0
  cloudflareStatusAt = 0
  await refreshCloudflareStatus()
  await Promise.all([loadCloudflareZones(true), loadCloudflareResources(true)])
}

async function cancelCloudflareLogin() {
  cloudflareBusy.value = 'login:cancel'
  cloudflareStatus.value = { ...(cloudflareStatus.value || {}), loginPhase: 'cancelled' }
  try { CancelCloudflareLogin() } finally {
    window.setTimeout(() => { void finishCloudflareLogin() }, 300)
  }
}

function openCloudflareDocs() {
  BrowserOpenURL('https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/')
}

watch(
  () => `${devTunnelStatus.value?.accountState || ''}|${devTunnelStatus.value?.accountLabel || ''}`,
  (next, prev) => {
    if (!prev || prev === next) return
    ++devTunnelCacheEpoch
    ++devTunnelResourceSeq
    ++devTunnelDetailSeq
    devTunnelListRequest = null
    devTunnelDetailCache.clear()
    devTunnelDetail.value = null
    devTunnelResourceCacheAt = 0
    devTunnelResources.state = 'idle'
    devTunnelResources.items = []
    devTunnelResources.detail = ''
    devTunnelCreated.value = null
    if (['providers', 'tunnels', 'overview'].includes(activePage.value)) void loadDevTunnelResources(false)
  },
)

watch(providerTab, (tab) => {
  if (tab === 'devtunnel') void loadDevTunnelResources(false)
  if (tab === 'cloudflare') {
    void refreshCloudflareStatus(false)
    void loadCloudflareResources(false)
    void loadCloudflareZones(false)
  }
})

async function installDevTunnel() {
  devTunnelBusy.value = 'install'
  try {
    await InstallDevTunnelCLI()
  } catch (err) {
    await refreshDevTunnelStatus()
    devTunnelStatus.value = { ...(devTunnelStatus.value || {}), detail: errorMessage(err) }
    return
  } finally {
    devTunnelBusy.value = ''
  }
  await refreshDevTunnelStatus()
}

function cancelDevTunnelInstall() {
  CancelDevTunnelInstall()
  devTunnelBusy.value = ''
}

async function loginDevTunnel(kind) {
  stopDevTunnelLoginPoll()
  devTunnelBusy.value = `login:${kind}`
  devTunnelStatus.value = {
    ...(devTunnelStatus.value || {}),
    loginInProgress: true,
    loginPhase: 'starting',
    loginError: '',
    loginUrl: '',
  }
  try {
    await StartDevTunnelLogin(kind)
    startDevTunnelLoginPoll()
  } catch (err) {
    devTunnelBusy.value = ''
    devTunnelStatus.value = {
      ...(devTunnelStatus.value || {}),
      loginInProgress: false,
      loginPhase: '',
      loginError: errorMessage(err),
    }
  }
}

function startDevTunnelLoginPoll() {
  stopDevTunnelLoginPoll()
  devTunnelLoginPoll = window.setInterval(() => {
    void pollDevTunnelLogin()
  }, 2000)
}

function stopDevTunnelLoginPoll() {
  if (devTunnelLoginPoll !== null) {
    window.clearInterval(devTunnelLoginPoll)
    devTunnelLoginPoll = null
  }
}

async function pollDevTunnelLogin() {
  if (devTunnelLoginPollInFlight) return
  devTunnelLoginPollInFlight = true
  try {
    await finishDevTunnelLogin()
  } finally {
    devTunnelLoginPollInFlight = false
  }
}

async function finishDevTunnelLogin() {
  await refreshDevTunnelStatus()
  const status = devTunnelStatus.value || {}
  if (!status.loginInProgress) {
    if (!String(devTunnelBusy.value).startsWith('logout')) devTunnelBusy.value = ''
    stopDevTunnelLoginPoll()
  }
}

async function cancelDevTunnelLogin() {
  devTunnelBusy.value = 'login:cancel'
  devTunnelStatus.value = {
    ...(devTunnelStatus.value || {}),
    loginPhase: 'cancelled',
  }
  try {
    CancelDevTunnelLogin()
  } finally {
    window.setTimeout(() => {
      void finishDevTunnelLogin()
    }, 300)
  }
}

function openDevTunnelLoginURL(url) {
  if (url) BrowserOpenURL(url)
}

async function logoutDevTunnel() {
  devTunnelBusy.value = 'logout'
  devTunnelStatus.value = {
    ...(devTunnelStatus.value || {}),
    loginInProgress: false,
    loginPhase: '',
  }
  try {
    await LogoutDevTunnel()
  } catch (err) {
    devTunnelStatus.value = { ...(devTunnelStatus.value || {}), detail: errorMessage(err) }
  } finally {
    devTunnelBusy.value = ''
    await refreshDevTunnelStatus()
  }
}

function openDevTunnelDocs() {
  BrowserOpenURL('https://learn.microsoft.com/azure/developer/dev-tunnels/get-started')
}

function setThemeBySwitch(enabled) {
  theme.value = enabled ? 'dark' : 'light'
  trackButtonClick('theme_switch', 'config', { theme: theme.value })
  logEvent('info', `Theme switched to ${theme.value}`)
}

function setAccentColor(color) {
  if (color === 'blue' || color === 'green') accentColor.value = color
}

function setConfigMessage(msg) {
  configMessage.value = msg || ''
}

function hideConfigToast() {
  showConfigToast.value = false
  if (configToastTimer !== null) {
    window.clearTimeout(configToastTimer)
    configToastTimer = null
  }
}

async function refreshLicenseStatus(options = {}) {
  const { silent = false } = options
  const revision = licenseStateRevision
  try {
    const status = await GetLicenseStatusAPI()
    if (revision !== licenseStateRevision || redeemDialog.submitting) return null
    if (!status || typeof status.active !== 'boolean') throw new Error(t('purchase.statusFailed'))
    applyLicenseState(status)
    if (!silent) {
      if (proLicense.isPro) {
        configMessage.value = t('config.messages.licenseActive', { expiry: proExpiryLabel.value })
        logEvent('info', `License status refreshed: active (${proExpiryLabel.value})`)
      } else {
        configMessage.value = t('config.messages.licenseInactive')
        logEvent('info', 'License status refreshed: inactive')
      }
    }
    return true
  } catch (err) {
    if (!silent) {
      const message = errorMessage(err, 'Failed to query license status from Wails backend.')
      configMessage.value = message
      logEvent('error', message)
    }
    return false
  }
}

async function refreshLicenseStatusFromConfig() {
  if (isRefreshingLicenseStatus.value) return
  isRefreshingLicenseStatus.value = true
  try {
    await refreshLicenseStatus()
  } finally {
    isRefreshingLicenseStatus.value = false
  }
}

async function loadStoredLicenseCode() {
  try {
    const code = String((await GetStoredLicenseCode()) || '').trim()
    if (code) proLicense.code = code
  } catch (_) {
    // local display cache is optional
  }
}

async function checkForUpdates() {
  if (isStoreDistribution.value) {
    setConfigMessage(t('config.storeUpdatesManaged'))
    return
  }
  if (isCheckingUpdates.value) return
  isCheckingUpdates.value = true
  trackButtonClick('check_updates', 'config')
  try {
    const result = await CheckForUpdatesAPI(appMeta.version)

    if (!result?.hasUpdate) {
      releasePageUrl.value = DEFAULT_RELEASES_PAGE_URL
      updateCheckDialog.mode = 'upToDate'
      updateCheckDialog.latestVersion = ''
      updateCheckDialog.message = ''
      updateCheckDialog.visible = true
      hasNewVersion.value = false
      logEvent('info', 'No updates available')
      return
    }

    hasNewVersion.value = true
    releasePageUrl.value =
      String(result.releasePageUrl || DEFAULT_RELEASES_PAGE_URL).trim() || DEFAULT_RELEASES_PAGE_URL
    updateCheckDialog.mode = 'updateAvailable'
    updateCheckDialog.latestVersion = String(result.latestVersion || '').trim()
    updateCheckDialog.message = ''
    updateCheckDialog.visible = true
    logEvent('info', `Update found: ${result.latestVersion}`)
    if (result.releaseNotes) {
      logEvent('info', `Release notes: ${result.releaseNotes}`)
    }
  } catch (err) {
    releasePageUrl.value = DEFAULT_RELEASES_PAGE_URL
    const message = errorMessage(err, 'Failed to check updates from GitHub Releases API.')
    updateCheckDialog.mode = 'error'
    updateCheckDialog.latestVersion = ''
    updateCheckDialog.message = message
    updateCheckDialog.visible = true
    hasNewVersion.value = false
    logEvent('error', message)
  } finally {
    isCheckingUpdates.value = false
  }
}

async function checkForUpdatesSilently() {
  if (isStoreDistribution.value) return
  try {
    const result = await CheckForUpdatesAPI(appMeta.version)
    if (result?.hasUpdate) {
      hasNewVersion.value = true
      releasePageUrl.value =
        String(result.releasePageUrl || DEFAULT_RELEASES_PAGE_URL).trim() || DEFAULT_RELEASES_PAGE_URL
    }
  } catch (_) {
    // silent check, ignore errors
  }
}

function openReleasePage() {
  if (isStoreDistribution.value) return
  openExternalUrl(releasePageUrl.value || DEFAULT_RELEASES_PAGE_URL)
}

function closeUpdateCheckDialog() {
  updateCheckDialog.visible = false
}

function openProUpgrade(tab = 'purchase') {
  if (proLicense.isPro) {
    configMessage.value = t('config.messages.proActive', { expiry: proExpiryLabel.value })
    logEvent('info', `Checked Pro expiry (${proExpiryLabel.value})`)
    void refreshLicenseStatus()
    return
  }

  if (redeemDialog.visible) return
  openRedeemDialog(tab)
  void refreshLicenseStatus({ silent: true }).then((ok) => {
    if (ok === false) configMessage.value = t('purchase.statusFailed')
    if (ok && proLicense.isPro && !redeemDialog.submitting) {
      closeRedeemDialog(true)
      configMessage.value = t('config.messages.proActive', { expiry: proExpiryLabel.value })
      logEvent('info', `Checked Pro expiry (${proExpiryLabel.value})`)
    }
  })
}

function resetJumperValidation() {
  jumperValidationError.value = ''
}

function resetInlineJumperValidation() {
  inlineJumperValidationError.value = ''
}

function resetJumperTest() {
  jumperTest.status = 'idle'
  jumperTest.message = ''
  jumperTest.debuggable = false
}

function resetTunnelTest() {
  tunnelTest.status = 'idle'
  tunnelTest.message = ''
  tunnelTest.debuggable = false
}

function resetAIDebugState(state) {
  state.status = 'idle'
  state.error = ''
  state.result = null
}

function ensureTunnelErrorAIDebugState(tunnelId) {
  const key = String(tunnelId)
  if (!tunnelErrorAiDebugStates[key]) {
    tunnelErrorAiDebugStates[key] = defaultAIDebugState()
  }
  return tunnelErrorAiDebugStates[key]
}

function setAIDebugLoading(state) {
  state.status = 'analyzing'
  state.error = ''
  state.result = null
}

function setAIDebugResult(state, result) {
  state.status = 'success'
  state.error = ''
  state.result = result || null
}

function setAIDebugError(state, message) {
  state.status = 'error'
  state.error = message
  state.result = null
}

function buildTunnelPayload(status = 'stopped') {
  const provider = tunnelForm.provider || 'ssh'
  const payload = {
    name: tunnelForm.name.trim(),
    provider,
    groupId: Number(tunnelForm.groupId) || 0,
    mode: provider === 'devtunnel' && !tunnelForm.portLocked ? 'auto' : tunnelForm.mode,
    jumperIds: provider === 'ssh' ? normalizeJumperIdList(tunnelForm.jumperIds) : [],
    localHost: tunnelForm.localHost.trim(),
    localPort: Number(tunnelForm.localPort),
    remoteHost: provider === 'ssh' ? tunnelForm.remoteHost.trim() : '',
    remotePort: provider === 'ssh' ? Number(tunnelForm.remotePort) : 0,
    autoStart: !!tunnelForm.autoStart,
    status,
    description: tunnelForm.description.trim(),
    devTunnel: null,
    cloudflare: null,
    frp: provider === 'frp' ? {
      serverId: tunnelForm.frpServerId,
      proxyName: (tunnelForm.frpProxyName || tunnelForm.name).trim(),
      remotePort: Number(tunnelForm.frpRemotePort),
      domain: String(tunnelForm.frpDomain || '').trim().toLowerCase(),
    } : null,
  }
  if (provider === 'devtunnel') {
    const idMode = tunnelForm.tunnelIdMode === 'bind' ? 'bind' : tunnelForm.tunnelIdMode === 'custom' ? 'custom' : 'auto'
    payload.devTunnelIdMode = idMode
    payload.devTunnel = {
      tunnelId: idMode === 'bind' ? tunnelForm.tunnelId || '' : '',
      tunnelName: idMode === 'custom' ? String(tunnelForm.devTunnelName || '').trim().toLowerCase() : '',
      localHost: payload.localHost,
      localPort: payload.localPort,
      protocol: payload.mode,
      access: tunnelForm.access || 'private',
      expiration: `${tunnelForm.expirationAmount}${tunnelForm.expirationUnit === 'h' ? 'h' : 'd'}`,
      region: idMode === 'bind' ? '' : (tunnelForm.region || ''),
    }
  }
  if (provider === 'cloudflare') {
    const idMode = tunnelForm.tunnelIdMode === 'bind' ? 'bind' : 'auto'
    payload.cloudflareIdMode = idMode
    payload.cloudflare = {
      tunnelId: idMode === 'bind' ? tunnelForm.tunnelId || '' : '',
      tunnelName: String(tunnelForm.tunnelName || '').trim(),
      localHost: payload.localHost,
      localPort: payload.localPort,
      protocol: payload.mode,
      hostname: normalizeHostname(tunnelForm.hostname),
    }
  }
  return payload
}

function buildTunnelPayloadForTest() {
  return buildTunnelPayload('stopped')
}

function buildJumperPayload(form) {
  const payload = {
    name: form.name.trim(),
    host: form.host.trim(),
    port: Number(form.port),
    user: form.user.trim(),
    authType: form.authType,
    keyPath: form.keyPath.trim(),
    agentSocketPath: form.agentSocketPath.trim(),
    password: form.password,
    bypassHostVerification: !!form.bypassHostVerification,
    keepAliveIntervalMs: Number(form.keepAliveIntervalMs),
    timeoutMs: Number(form.timeoutMs),
    notes: form.notes.trim(),
  }

  if (!authNeedsKeyFile(payload.authType)) payload.keyPath = ''
  if (!authShowsPassword(payload.authType)) payload.password = ''
  return payload
}

function validateJumperPayload(payload) {
  if (!payload.name) return 'Name is required.'
  if (nameUnits(payload.name) > JUMPER_LIMITS.name) return 'Name must be <= 20 chars or <= 10 Chinese chars.'
  if (!payload.host) return 'Host is required.'
  if (payload.host.length > JUMPER_LIMITS.host) return `Host length must be <= ${JUMPER_LIMITS.host}.`
  if (!payload.user) return 'User is required.'
  if (payload.user.length > JUMPER_LIMITS.user) return `User length must be <= ${JUMPER_LIMITS.user}.`
  if (!Number.isInteger(payload.port) || payload.port < 1 || payload.port > 65535) {
    return 'Port must be between 1 and 65535.'
  }
  if (payload.keyPath.length > JUMPER_LIMITS.keyPath) return `Key path length must be <= ${JUMPER_LIMITS.keyPath}.`
  if (payload.agentSocketPath.length > JUMPER_LIMITS.agentSocketPath) {
    return `Agent socket path length must be <= ${JUMPER_LIMITS.agentSocketPath}.`
  }
  if (payload.password.length > JUMPER_LIMITS.password) return `Password length must be <= ${JUMPER_LIMITS.password}.`
  if (payload.notes.length > JUMPER_LIMITS.notes) return `Notes length must be <= ${JUMPER_LIMITS.notes}.`
  if (authNeedsKeyFile(payload.authType) && !payload.keyPath) {
    return 'SSH Key mode requires selecting a key file.'
  }
  if (authNeedsPassword(payload.authType) && !payload.password) {
    return 'Current auth method requires a password.'
  }
  if (
    !Number.isInteger(payload.keepAliveIntervalMs) ||
    payload.keepAliveIntervalMs > JUMPER_LIMITS.keepAliveIntervalMax
  ) {
    return `KeepAlive interval(ms) must be 0 (disable) or between 1000 and ${JUMPER_LIMITS.keepAliveIntervalMax}.`
  }
  if (payload.keepAliveIntervalMs > 0 && payload.keepAliveIntervalMs < 1000) {
    return `KeepAlive interval(ms) must be 0 (disable) or between 1000 and ${JUMPER_LIMITS.keepAliveIntervalMax}.`
  }
  if (
    !Number.isInteger(payload.timeoutMs) ||
    payload.timeoutMs < JUMPER_LIMITS.timeoutMin ||
    payload.timeoutMs > JUMPER_LIMITS.timeoutMax
  ) {
    return `Timeout(ms) must be between ${JUMPER_LIMITS.timeoutMin} and ${JUMPER_LIMITS.timeoutMax}.`
  }
  return ''
}

function onJumperKeyFileChange(event) {
  const file = event.target.files && event.target.files[0]
  if (file) jumperForm.keyPath = file.name
}

function onInlineJumperKeyFileChange(event) {
  const file = event.target.files && event.target.files[0]
  if (file) inlineJumperForm.keyPath = file.name
}

function openNewJumper() {
  editingJumperId.value = null
  Object.assign(jumperForm, defaultJumperForm())
  showJumperBasic.value = true
  showJumperAdvanced.value = false
  resetJumperValidation()
  resetJumperTest()
  resetAIDebugState(jumperAiDebug)
  showJumperModal.value = true
  trackModalOpen('jumper_create', 'jumpers')
}

async function loadImportJumperSources() {
  importJumperError.value = ''
  try {
    const sources = await GetSSHConfigImportSources()
    sshConfigSources.value = Array.isArray(sources) ? sources : []
    selectedImportJumperSourcePath.value = sshConfigSources.value[0]?.path || ''
  } catch (err) {
    sshConfigSources.value = []
    selectedImportJumperSourcePath.value = ''
    importJumperError.value = errorMessage(err, 'Failed to load SSH config sources')
  }
}

async function loadImportJumpers() {
  const targetPath = String(selectedImportJumperSourcePath.value || '').trim()
  if (!targetPath) return

  importJumperLoading.value = true
  importJumperError.value = ''
  importJumperHasLoaded.value = false
  try {
    const result = await LoadSSHConfigJumpersByPath(targetPath)
    sshConfigCandidates.value = Array.isArray(result?.candidates) ? result.candidates : []
    importJumperHasLoaded.value = true
  } catch (err) {
    sshConfigCandidates.value = []
    importJumperError.value = errorMessage(err, 'Failed to load SSH config jumpers')
  } finally {
    importJumperLoading.value = false
  }
}

function openImportJumper() {
  showImportJumperModal.value = true
  trackModalOpen('jumper_import', 'jumpers')
  importJumperLoading.value = false
  importJumperError.value = ''
  importJumperHasLoaded.value = false
  sshConfigCandidates.value = []
  void loadImportJumperSources()
}

function closeImportJumper() {
  showImportJumperModal.value = false
  importJumperLoading.value = false
  importJumperError.value = ''
  importJumperHasLoaded.value = false
  sshConfigSources.value = []
  selectedImportJumperSourcePath.value = ''
  sshConfigCandidates.value = []
}

function editJumper(jumper) {
  editingJumperId.value = jumper.id
  Object.assign(jumperForm, defaultJumperForm(), jumper)
  showJumperBasic.value = true
  showJumperAdvanced.value = false
  resetJumperValidation()
  resetJumperTest()
  resetAIDebugState(jumperAiDebug)
  showJumperModal.value = true
  trackModalOpen('jumper_edit', 'jumpers', { jumper_name: jumper.name })
}

function fillJumperFormFromJumper(jumper, nameOverride = null) {
  Object.assign(jumperForm, {
    name: nameOverride ?? jumper.name,
    host: jumper.host,
    port: jumper.port,
    user: jumper.user,
    authType: jumper.authType,
    keyPath: jumper.keyPath || '',
    agentSocketPath: jumper.agentSocketPath || '',
    password: jumper.password || '',
    bypassHostVerification: !!jumper.bypassHostVerification,
    keepAliveIntervalMs: jumper.keepAliveIntervalMs,
    timeoutMs: jumper.timeoutMs,
    notes: jumper.notes || '',
  })
}

function copyJumper(jumper) {
  editingJumperId.value = null
  fillJumperFormFromJumper(jumper, `copy-${jumper.name}`)
  showJumperBasic.value = true
  showJumperAdvanced.value = false
  resetJumperValidation()
  resetJumperTest()
  resetAIDebugState(jumperAiDebug)
  showJumperModal.value = true
}

async function saveJumper() {
  resetJumperValidation()
  const payload = buildJumperPayload(jumperForm)
  const error = validateJumperPayload(payload)
  if (error) {
    jumperValidationError.value = error
    return
  }

  try {
    if (editingJumperId.value) {
      await UpdateJumper(editingJumperId.value, payload)
      logEvent('info', `Jumper ${payload.name} updated`)
    } else {
      const created = await CreateJumper(payload)
      logEvent('info', `Jumper ${created.name} created`)
    }

    await loadStateFromBackend()
    showJumperModal.value = false
  } catch (err) {
    jumperValidationError.value = errorMessage(err)
  }
}

async function importJumpers(jumpersToImport) {
  try {
    importJumperError.value = ''
    let importedCount = 0
    let skippedCount = 0

    const existingSignatures = new Set(
      jumpers.value.map(
        (item) =>
          `${String(item.host || '')
            .trim()
            .toLowerCase()}|${String(item.user || '').trim()}|${Number(item.port) || 22}`,
      ),
    )

    for (const item of jumpersToImport) {
      const payload = {
        name: String(item.name || '').trim(),
        host: String(item.host || '').trim(),
        port: Number(item.port) || 22,
        user: String(item.user || '').trim(),
        authType: item.authType || 'ssh_agent',
        keyPath: String(item.keyPath || '').trim(),
        agentSocketPath: String(item.agentSocketPath || '').trim(),
        password: '',
        bypassHostVerification: !!item.bypassHostVerification,
        keepAliveIntervalMs: Number(item.keepAliveIntervalMs) || 5000,
        timeoutMs: Number(item.timeoutMs) || 5000,
        hostKeyAlgorithms: String(item.hostKeyAlgorithms || '').trim(),
        notes: `Imported from SSH config alias "${item.alias}" on ${new Date().toLocaleDateString()}`,
      }

      const signature = `${payload.host.toLowerCase()}|${payload.user}|${payload.port}`
      if (existingSignatures.has(signature)) {
        skippedCount++
        continue
      }

      await CreateJumper(payload)
      existingSignatures.add(signature)
      importedCount++
      logEvent('info', `Jumper ${payload.name} imported from SSH config`)
    }

    await loadStateFromBackend()
    closeImportJumper()

    let message = `Successfully imported ${importedCount} jumper(s)`
    if (skippedCount > 0) {
      message = `${message}; skipped ${skippedCount} duplicate jumper(s)`
    }
    logEvent('info', message)
  } catch (err) {
    const message = errorMessage(err, 'Failed to import jumpers')
    importJumperError.value = message
    logEvent('error', message)
  }
}

async function testJumperConnection() {
  resetJumperValidation()
  resetAIDebugState(jumperAiDebug)
  const payload = buildJumperPayload(jumperForm)
  const error = validateJumperPayload(payload)
  if (error) {
    jumperTest.status = 'error'
    jumperTest.message = error
    jumperTest.debuggable = false
    return
  }

  resetJumperTest()
  jumperTest.status = 'testing'
  jumperTest.message = t('app.modals.jumper.testing')
  try {
    await TestJumperConnectionAPI(payload)
    jumperTest.status = 'success'
    jumperTest.message = 'Connection test passed.'
    jumperTest.debuggable = false
    logEvent('info', `Connection test passed for jumper ${payload.name}`)
  } catch (err) {
    jumperTest.status = 'error'
    jumperTest.message = errorMessage(err)
    jumperTest.debuggable = AI_DEBUG_ENABLED
    logEvent('error', `Connection test failed for jumper ${payload.name}: ${jumperTest.message}`)
  }
}

async function testTunnelConnection() {
  resetInlineJumperValidation()
  tunnelValidationError.value = ''
  resetAIDebugState(tunnelAiDebug)

  if (['devtunnel', 'cloudflare', 'frp'].includes(tunnelForm.provider)) {
    const payload = buildTunnelPayloadForTest()
    if (!payload.name || !payload.localHost || !payload.localPort) {
      tunnelTest.status = 'error'
      tunnelTest.message = t('app.modals.tunnel.testRequiredFields')
      tunnelTest.debuggable = false
      return
    }
    if (tunnelForm.provider !== 'frp' && !isValidTargetHost(payload.localHost)) {
      tunnelTest.status = 'error'
      tunnelTest.message = t('app.modals.tunnel.invalidTargetHost')
      tunnelTest.debuggable = false
      return
    }
    resetTunnelTest()
    tunnelTest.status = 'testing'
    tunnelTest.message = t('app.modals.tunnel.testing')
    try {
      await TestTunnelConnectionAPI(payload, null)
      tunnelTest.status = 'success'
      tunnelTest.message = t('app.modals.tunnel.testPassedLocal')
      tunnelTest.debuggable = false
    } catch (err) {
      tunnelTest.status = 'error'
      tunnelTest.message = errorMessage(err)
      tunnelTest.debuggable = false
    }
    return
  }

  let inlinePayload = null
  let selectedJumperIds = normalizeJumperIdList(tunnelForm.jumperIds)

  if (tunnelForm.appendNewJumper) {
    inlinePayload = buildJumperPayload(inlineJumperForm)
    const inlineError = validateJumperPayload(inlinePayload)
    if (inlineError) {
      tunnelTest.status = 'error'
      tunnelTest.message = `[Jumper] ${inlineError}`
      tunnelTest.debuggable = false
      return
    }
  }

  if (!selectedJumperIds.length && !inlinePayload) {
    tunnelTest.status = 'error'
    tunnelTest.message = t('app.modals.tunnel.testSelectJumper')
    tunnelTest.debuggable = false
    return
  }

  const payload = buildTunnelPayloadForTest()
  payload.jumperIds = selectedJumperIds

  if (!payload.name || !payload.localHost || !payload.localPort) {
    tunnelTest.status = 'error'
    tunnelTest.message = t('app.modals.tunnel.testRequiredFields')
    tunnelTest.debuggable = false
    return
  }
  if (payload.mode !== 'dynamic' && (!payload.remoteHost || !payload.remotePort)) {
    tunnelTest.status = 'error'
    tunnelTest.message = t('app.modals.tunnel.testRequiredRemote')
    tunnelTest.debuggable = false
    return
  }

  resetTunnelTest()
  tunnelTest.status = 'testing'
  tunnelTest.message = t('app.modals.tunnel.testing')
  try {
    const result = await TestTunnelConnectionAPI(payload, inlinePayload)
    const latencyText = formatLatencyLabel(result?.latencyMs)
    tunnelTest.status = 'success'
    tunnelTest.message = t('app.modals.tunnel.testPassedWithLatency', { latency: latencyText })
    tunnelTest.debuggable = false
    logEvent('info', `Connection test passed for tunnel ${payload.name}; latency=${latencyText}`)
  } catch (err) {
    tunnelTest.status = 'error'
    tunnelTest.message = errorMessage(err)
    tunnelTest.debuggable = AI_DEBUG_ENABLED
    logEvent('error', `Connection test failed for tunnel ${payload.name}: ${tunnelTest.message}`)
  }
}

async function runJumperAIDebug() {
  if (!AI_DEBUG_ENABLED || !jumperTest.debuggable || !jumperTest.message) return
  const payload = buildJumperPayload(jumperForm)
  setAIDebugLoading(jumperAiDebug)
  try {
    const result = await DebugJumperFailureAPI(payload, jumperTest.message, locale.value)
    setAIDebugResult(jumperAiDebug, result)
    logEvent('info', `AI Debug completed for jumper ${payload.name}`)
  } catch (err) {
    const message = aiDebugErrorMessage(err, 'AI Debug failed for this jumper.')
    setAIDebugError(jumperAiDebug, message)
    logEvent('error', message)
  }
}

async function runTunnelAIDebug() {
  if (!AI_DEBUG_ENABLED || !tunnelTest.debuggable || !tunnelTest.message) return

  let inlinePayload = null
  if (tunnelForm.appendNewJumper) {
    inlinePayload = buildJumperPayload(inlineJumperForm)
  }
  const payload = buildTunnelPayloadForTest()
  setAIDebugLoading(tunnelAiDebug)
  try {
    const result = await DebugTunnelFailureAPI(payload, inlinePayload, tunnelTest.message, locale.value)
    setAIDebugResult(tunnelAiDebug, result)
    logEvent('info', `AI Debug completed for tunnel ${payload.name}`)
  } catch (err) {
    const message = aiDebugErrorMessage(err, 'AI Debug failed for this tunnel.')
    setAIDebugError(tunnelAiDebug, message)
    logEvent('error', message)
  }
}

async function runSavedTunnelAIDebug(tunnel) {
  if (!AI_DEBUG_ENABLED || !tunnel?.id || !tunnel?.lastError) return
  const state = ensureTunnelErrorAIDebugState(tunnel.id)
  setAIDebugLoading(state)
  try {
    const result = await DebugSavedTunnelFailureAPI(Number(tunnel.id), String(tunnel.lastError || ''), locale.value)
    setAIDebugResult(state, result)
    logEvent('info', `AI Debug completed for tunnel ${tunnel.name}`)
  } catch (err) {
    const message = aiDebugErrorMessage(err, `AI Debug failed for tunnel ${tunnel?.name || ''}`.trim())
    setAIDebugError(state, message)
    logEvent('error', message)
  }
}

function openSavedTunnelAIDebug(tunnel) {
  if (!AI_DEBUG_ENABLED || !tunnel?.id || !tunnel?.lastError) return
  selectedAIDebugTunnel.value = tunnel
  const state = ensureTunnelErrorAIDebugState(tunnel.id)
  if (state.status === 'idle') {
    void runSavedTunnelAIDebug(tunnel)
  }
}

function closeSavedTunnelAIDebug() {
  selectedAIDebugTunnel.value = null
}

function retrySavedTunnelAIDebug() {
  if (!selectedAIDebugTunnel.value) return
  void runSavedTunnelAIDebug(selectedAIDebugTunnel.value)
}

function retestSavedTunnelFromAIDebug() {
  if (!selectedAIDebugTunnel.value) return
  void toggleTunnel(selectedAIDebugTunnel.value)
}

function resetProviderDrafts() {
  providerDrafts.frp = null
  providerDrafts.ssh = null
  providerDrafts.devtunnel = null
  providerDrafts.cloudflare = null
}

function captureProviderDraft(provider) {
  if (provider === 'frp') {
    return {
      mode: tunnelForm.mode,
      localHost: tunnelForm.localHost,
      localPort: tunnelForm.localPort,
      frpServerId: tunnelForm.frpServerId,
      frpProxyName: tunnelForm.frpProxyName,
      frpRemotePort: tunnelForm.frpRemotePort,
      frpDomain: tunnelForm.frpDomain,
      appendNewJumper: false,
    }
  }
  if (provider === 'devtunnel') {
    return {
      mode: tunnelForm.mode,
      localHost: tunnelForm.localHost,
      localPort: tunnelForm.localPort,
      access: tunnelForm.access,
      expirationAmount: tunnelForm.expirationAmount,
      expirationUnit: tunnelForm.expirationUnit,
      region: tunnelForm.region,
      tunnelId: tunnelForm.tunnelId,
      tunnelIdMode: tunnelForm.tunnelIdMode,
      devTunnelName: tunnelForm.devTunnelName,
      originalTunnelId: tunnelForm.originalTunnelId,
      portLocked: tunnelForm.portLocked,
      tunnelPort: tunnelForm.tunnelPort,
      remoteExpiration: tunnelForm.remoteExpiration,
      autoStart: tunnelForm.autoStart,
      description: tunnelForm.description,
    }
  }
  if (provider === 'cloudflare') {
    return {
      mode: tunnelForm.mode,
      localHost: tunnelForm.localHost,
      localPort: tunnelForm.localPort,
      tunnelId: tunnelForm.tunnelId,
      tunnelIdMode: tunnelForm.tunnelIdMode,
      originalTunnelId: tunnelForm.originalTunnelId,
      tunnelName: tunnelForm.tunnelName,
      hostname: tunnelForm.hostname,
      autoStart: tunnelForm.autoStart,
      description: tunnelForm.description,
    }
  }
  return {
    mode: tunnelForm.mode,
    jumperIds: [...normalizeJumperIdList(tunnelForm.jumperIds)],
    nextJumperId: tunnelForm.nextJumperId,
    appendNewJumper: tunnelForm.appendNewJumper,
    localHost: tunnelForm.localHost,
    localPort: tunnelForm.localPort,
    remoteHost: tunnelForm.remoteHost,
    remotePort: tunnelForm.remotePort,
  }
}

function applyProviderDraft(provider) {
  const draft = providerDrafts[provider]
  if (!draft) {
    if (provider === 'frp') Object.assign(tunnelForm, { mode: tunnelForm.mode === 'http' || tunnelForm.mode === 'https' ? 'http' : 'tcp', localHost: tunnelForm.localHost || '127.0.0.1', localPort: Number(tunnelForm.localPort) || 3000, frpServerId: tunnelForm.frpServerId || frpState.servers[0]?.id || '', frpProxyName:'', frpRemotePort: Number(tunnelForm.frpRemotePort) || 13000, frpDomain: tunnelForm.frpDomain || '', jumperIds:[], appendNewJumper:false, remoteHost:'', remotePort:0 })
    if (provider === 'devtunnel') {
      Object.assign(tunnelForm, {
        mode: 'auto',
        localHost: '127.0.0.1',
        localPort: 3000,
        access: 'private',
        expirationAmount: 30,
        expirationUnit: 'd',
        region: '',
        tunnelId: '',
        tunnelIdMode: 'auto',
        devTunnelName: '',
        originalTunnelId: '',
        portLocked: false,
        tunnelPort: 0,
        resourceError: '',
        resourceNote: t('app.modals.tunnel.autoCreateHint'),
        remoteExpiration: '',
        remoteHost: '',
        remotePort: 0,
        jumperIds: [],
        appendNewJumper: false,
      })
    }
    if (provider === 'cloudflare') {
      Object.assign(tunnelForm, {
        mode: 'http',
        localHost: '127.0.0.1',
        localPort: 3000,
        tunnelId: '',
        tunnelIdMode: 'auto',
        originalTunnelId: '',
        tunnelName: '',
        hostname: '',
        resourceError: '',
        resourceNote: t('app.cloudflare.autoCreate'),
        remoteHost: '',
        remotePort: 0,
        jumperIds: [],
        appendNewJumper: false,
        autoStart: false,
        description: '',
      })
    }
    return
  }
  Object.assign(tunnelForm, draft)
}

function onTunnelProviderChange(next) {
  const current = tunnelForm.provider || 'ssh'
  if (!next || next === current) return
  const fromSSH = current === 'ssh'
  const hasFRPDraft = Boolean(providerDrafts.frp)
  const localHost = tunnelForm.localHost
  const localPort = tunnelForm.localPort
  const remotePort = Number(tunnelForm.remotePort)
  providerDrafts[current] = captureProviderDraft(current)
  tunnelForm.provider = next
  applyProviderDraft(next)
  if (fromSSH && next === 'frp' && !hasFRPDraft) {
    tunnelForm.mode = 'tcp'
    tunnelForm.localHost = localHost || '127.0.0.1'
    tunnelForm.localPort = Number(localPort) || 3000
    if (remotePort >= 1 && remotePort <= 65535) tunnelForm.frpRemotePort = remotePort
  }
  resetTunnelTest()
  tunnelValidationError.value = ''
  if (next === 'cloudflare') {
    void refreshCloudflareStatus(false)
    void loadCloudflareResources(false)
    void loadCloudflareZones(false)
  }
}

const providerDrafts = { frp: null, ssh: null, devtunnel: null, cloudflare: null }

function openNewTunnel() {
  frpDraftPending.value = false
  editingTunnelId.value = null
  resetProviderDrafts()
  Object.assign(tunnelForm, defaultTunnelForm())
  Object.assign(inlineJumperForm, defaultInlineJumperForm())
  resetInlineJumperValidation()
  resetTunnelTest()
  resetAIDebugState(tunnelAiDebug)
  tunnelValidationError.value = ''
  tunnelForm.jumperIds = jumpers.value.length ? [jumpers.value[0].id] : []
  tunnelForm.nextJumperId = getNextTunnelJumperCandidate(tunnelForm.jumperIds)
  tunnelForm.appendNewJumper = jumpers.value.length === 0
  showTunnelModal.value = true
  trackModalOpen('tunnel_create', 'tunnels')
   void refreshFRP().catch(err => logEvent('error', String(err)))
  void loadDevTunnelResources(false)
}

function openNewFRPTunnel(server) {
  openNewTunnel()
  onTunnelProviderChange('frp')
  tunnelForm.frpServerId = server.id
}
const frpDraftPending = ref(false)
function resumeFRPDraft() { activePage.value = 'tunnels'; showTunnelModal.value = true; frpDraftPending.value = false }
function manageFRP() {
  frpDraftPending.value = true
  showTunnelModal.value = false
  openTunnelProvider('frp')
}
function viewFRPTunnels(server) {
  activePage.value = 'tunnels'
  tunnelSearchQuery.value = server.name
}
function openImportTunnel() {
  importTunnelError.value = ''
  showImportTunnelModal.value = true
  trackModalOpen('tunnel_import', 'tunnels')
}

function closeImportTunnel() {
  showImportTunnelModal.value = false
  importTunnelError.value = ''
}

async function importTunnels(tunnelsToImport) {
  try {
    importTunnelError.value = ''
    let importedCount = 0
    let skippedCount = 0
    let createdJumperCount = 0
    const existingSignatures = new Set(tunnels.value.map((item) => getTunnelImportSignature(item)))
    const createdJumperCache = new Map()

    for (const tunnelData of tunnelsToImport) {
      let jumperIds = []

      if (tunnelData.importJumper?.mode === 'existing') {
        const selectedJumperId = Number(tunnelData.importJumper.jumperId)
        const selectedJumper = jumpers.value.find((item) => item.id === selectedJumperId)
        if (!selectedJumper) {
          throw new Error('Selected existing jumper is missing. Please re-parse and try again.')
        }
        jumperIds = [selectedJumper.id]
      }

      if (tunnelData.importJumper?.mode === 'new' && tunnelData.importJumper?.payload) {
        const payload = tunnelData.importJumper.payload
        const existingJumper = jumpers.value.find((item) => {
          return (
            String(item.host || '')
              .trim()
              .toLowerCase() ===
              String(payload.host || '')
                .trim()
                .toLowerCase() &&
            String(item.user || '').trim() === String(payload.user || '').trim() &&
            Number(item.port) === Number(payload.port)
          )
        })

        if (existingJumper) {
          jumperIds = [existingJumper.id]
        } else {
          const cacheKey = JSON.stringify({
            host: String(payload.host || '')
              .trim()
              .toLowerCase(),
            user: String(payload.user || '').trim(),
            port: Number(payload.port),
            authType: payload.authType,
            keyPath: String(payload.keyPath || '').trim(),
            agentSocketPath: String(payload.agentSocketPath || '').trim(),
          })

          if (createdJumperCache.has(cacheKey)) {
            jumperIds = [createdJumperCache.get(cacheKey)]
          } else {
            const createdJumper = await CreateJumper(payload)
            jumperIds = [createdJumper.id]
            jumpers.value.push(createdJumper)
            createdJumperCache.set(cacheKey, createdJumper.id)
            createdJumperCount++
            logEvent('info', `Jumper ${createdJumper.name} created from import`)
          }
        }
      }

      // Backward-compatible fallback for old import payload.
      if (jumperIds.length === 0 && tunnelData.jumperConfig) {
        const config = tunnelData.jumperConfig
        const existingJumper = jumpers.value.find(
          (item) => item.host === config.host && item.user === config.user && item.port === config.port,
        )

        if (existingJumper) {
          jumperIds = [existingJumper.id]
        } else {
          const fallbackPayload = {
            name: `${config.host.split('.')[0]}-import`,
            host: config.host,
            port: config.port,
            user: config.user,
            authType: config.keyPath ? 'ssh_key' : 'ssh_agent',
            keyPath: config.keyPath || '',
            agentSocketPath: '',
            password: '',
            bypassHostVerification: true,
            keepAliveIntervalMs: config.keepAliveIntervalMs || 5000,
            timeoutMs: 5000,
            notes: `Imported from SSH command on ${new Date().toLocaleDateString()}`,
          }

          const createdJumper = await CreateJumper(fallbackPayload)
          jumperIds = [createdJumper.id]
          jumpers.value.push(createdJumper)
          createdJumperCount++
          logEvent('info', `Jumper ${createdJumper.name} created from import`)
        }
      }

      if (jumperIds.length === 0) {
        throw new Error(t('app.modals.importTunnel.errorMissingTarget'))
      }

      // Create the tunnel
      const payload = {
        name: tunnelData.name,
        mode: tunnelData.mode,
        jumperIds: jumperIds,
        localHost: tunnelData.localHost,
        localPort: tunnelData.localPort,
        remoteHost: tunnelData.remoteHost,
        remotePort: tunnelData.remotePort,
        autoStart: false,
        status: 'stopped',
        description: `Imported from SSH command on ${new Date().toLocaleDateString()}`,
      }

      const signature = getTunnelImportSignature(payload)
      if (existingSignatures.has(signature)) {
        skippedCount++
        logEvent('warn', `Tunnel ${payload.name} skipped (duplicate)`)
        continue
      }

      await CreateTunnel(payload)
      existingSignatures.add(signature)
      importedCount++
      logEvent('info', `Tunnel ${payload.name} imported`)
    }

    await loadStateFromBackend()
    closeImportTunnel()

    let message =
      createdJumperCount > 0
        ? `Successfully imported ${importedCount} tunnel(s) and created ${createdJumperCount} jumper(s)`
        : `Successfully imported ${importedCount} tunnel(s)`
    if (skippedCount > 0) {
      message = `${message}; skipped ${skippedCount} duplicate tunnel(s)`
    }
    logEvent('info', message)
  } catch (err) {
    const message = errorMessage(err, 'Failed to import tunnels')
    importTunnelError.value = message
    logEvent('error', message)
  }
}

function fillTunnelFormFromTunnel(tunnel, nameOverride = null, options = {}) {
  const provider = tunnelProvider(tunnel)
  const expiration = splitDevTunnelExpiration(tunnel.devTunnel?.expiration)
  const cloudflareID = options.clearTunnelId ? '' : tunnel.cloudflare?.tunnelId || ''
  const devID = options.clearTunnelId ? '' : tunnel.devTunnel?.tunnelId || ''
  const devName = options.clearTunnelId ? '' : String(tunnel.devTunnel?.tunnelName || '').trim().toLowerCase()
  const boundID = provider === 'cloudflare' ? cloudflareID : devID
  const devIDMode = options.clearTunnelId ? 'auto' : boundID ? 'bind' : devName ? 'custom' : 'auto'
  const hostname = options.clearTunnelId ? '' : tunnel.cloudflare?.hostname || ''
  Object.assign(tunnelForm, {
    name: nameOverride ?? tunnel.name,
    frpServerId: tunnel.frp?.serverId || '',
    frpProxyName: options.clearTunnelId ? '' : tunnel.frp?.proxyName || '',
    frpRemotePort: options.clearTunnelId ? null : tunnel.frp?.remotePort || 13000,
    frpDomain: options.clearTunnelId ? '' : tunnel.frp?.domain || '',
    provider,
    groupId: Number(tunnel.groupId) || 0,
    mode: provider === 'cloudflare'
      ? tunnel.cloudflare?.protocol || tunnel.mode || 'http'
      : provider === 'devtunnel'
        ? tunnel.devTunnel?.protocol || tunnel.mode || 'http'
        : tunnel.mode,
    jumperIds: provider === 'ssh' ? normalizeJumperIdList(tunnel.jumperIds) : [],
    nextJumperId: getNextTunnelJumperCandidate(tunnel.jumperIds),
    appendNewJumper: false,
    localHost: provider === 'cloudflare'
      ? tunnel.cloudflare?.localHost || tunnel.localHost || '127.0.0.1'
      : provider === 'devtunnel'
        ? tunnel.devTunnel?.localHost || tunnel.localHost || '127.0.0.1'
        : tunnel.localHost,
    localPort: provider === 'cloudflare'
      ? tunnel.cloudflare?.localPort || tunnel.localPort
      : provider === 'devtunnel'
        ? tunnel.devTunnel?.localPort || tunnel.localPort
        : tunnel.localPort,
    remoteHost: provider === 'ssh' ? tunnel.remoteHost : '',
    remotePort: provider === 'ssh' ? tunnel.remotePort : 0,
    access: tunnel.devTunnel?.access || 'private',
    expirationAmount: expiration.amount,
    expirationUnit: expiration.unit,
    region: options.clearTunnelId || boundID ? '' : (tunnel.devTunnel?.region || ''),
    tunnelId: boundID,
    tunnelIdMode: provider === 'cloudflare'
      ? (options.clearTunnelId || !boundID ? 'auto' : 'bind')
      : devIDMode,
    devTunnelName: provider === 'cloudflare' ? '' : devName,
    originalTunnelId: boundID,
    tunnelName: options.clearTunnelId ? '' : tunnel.cloudflare?.tunnelName || '',
    hostname,
    portLocked: false,
    tunnelPort: options.clearTunnelId ? 0 : Number(tunnel.devTunnel?.tunnelPort || 0),
    resourceError: '',
    resourceNote: options.clearTunnelId || !boundID
      ? (provider === 'cloudflare'
        ? t('app.cloudflare.autoCreate')
        : devName ? '' : t('app.modals.tunnel.autoCreateHint'))
      : '',
    remoteExpiration: '',
    publicUrl: provider === 'cloudflare' ? cloudflarePublicURL(hostname) : options.clearTunnelId ? '' : String(tunnel.publicUrl || tunnel.devTunnel?.lastPublicUrl || '').trim(),
    autoStart: tunnel.autoStart,
    description: tunnel.description,
  })
}

function editTunnel(tunnel) {
  frpDraftPending.value = false
  editingTunnelId.value = tunnel.id
  resetProviderDrafts()
  fillTunnelFormFromTunnel(tunnel)
  Object.assign(inlineJumperForm, defaultInlineJumperForm())
  resetInlineJumperValidation()
  resetTunnelTest()
  resetAIDebugState(tunnelAiDebug)
  tunnelValidationError.value = ''
  showTunnelModal.value = true
  trackModalOpen('tunnel_edit', 'tunnels', { tunnel_name: tunnel.name })
  if (tunnelProvider(tunnel) === 'devtunnel') {
    void loadDevTunnelResources(false)
    if (tunnel.devTunnel?.tunnelId) void inspectDevTunnelID(tunnel.devTunnel.tunnelId)
  }
  if (tunnelProvider(tunnel) === 'cloudflare') {
    void refreshCloudflareStatus(false)
    void loadCloudflareResources(false)
    void loadCloudflareZones(false)
    if (tunnel.cloudflare?.tunnelId) void inspectCloudflareID(tunnel.cloudflare.tunnelId)
  }
}

function addJumperToTunnelChain(jumperId) {
  const id = Number(jumperId)
  if (!Number.isInteger(id) || id <= 0) return
  const ids = normalizeJumperIdList(tunnelForm.jumperIds)
  if (ids.includes(id)) return
  tunnelForm.jumperIds = [...ids, id]
  tunnelForm.nextJumperId = getNextTunnelJumperCandidate(tunnelForm.jumperIds)
}

function setPrimaryJumperForTunnelChain(jumperId) {
  const id = Number(jumperId)
  if (!Number.isInteger(id) || id <= 0) return
  const ids = normalizeJumperIdList(tunnelForm.jumperIds)
  // Preserve other hops only when the user already has a multi-hop chain.
  tunnelForm.jumperIds = ids.length > 1 ? [id, ...ids.filter((item) => item !== id)] : [id]
  tunnelForm.nextJumperId = getNextTunnelJumperCandidate(tunnelForm.jumperIds)
}

function removeJumperFromTunnelChain(index) {
  const ids = normalizeJumperIdList(tunnelForm.jumperIds)
  if (!Number.isInteger(index) || index < 0 || index >= ids.length) return
  ids.splice(index, 1)
  tunnelForm.jumperIds = ids
  tunnelForm.nextJumperId = getNextTunnelJumperCandidate(tunnelForm.jumperIds)
}

function moveJumperInTunnelChain(index, offset) {
  const ids = normalizeJumperIdList(tunnelForm.jumperIds)
  if (!Number.isInteger(index) || index < 0 || index >= ids.length) return
  const step = Number(offset)
  if (!Number.isInteger(step) || step === 0) return
  const target = index + step
  if (target < 0 || target >= ids.length) return
  const temp = ids[target]
  ids[target] = ids[index]
  ids[index] = temp
  tunnelForm.jumperIds = ids
  tunnelForm.nextJumperId = getNextTunnelJumperCandidate(tunnelForm.jumperIds)
}

function trimJumperChainToPrimary() {
  const ids = normalizeJumperIdList(tunnelForm.jumperIds)
  if (ids.length <= 1) return
  tunnelForm.jumperIds = [ids[0]]
  tunnelForm.nextJumperId = getNextTunnelJumperCandidate(tunnelForm.jumperIds)
}

function copyTunnel(tunnel) {
  editingTunnelId.value = null
  resetProviderDrafts()
  fillTunnelFormFromTunnel(tunnel, `copy-${tunnel.name}`, { clearTunnelId: true })
  Object.assign(inlineJumperForm, defaultInlineJumperForm())
  resetInlineJumperValidation()
  resetTunnelTest()
  resetAIDebugState(tunnelAiDebug)
  tunnelValidationError.value = ''
  showTunnelModal.value = true
}

async function saveTunnel() {
  await persistTunnel()
}

async function persistTunnel() {
  const editor = document.getElementById('tunnel-editor-form')
  const provider = tunnelForm.provider || 'ssh'
  const nameInput = editor?.querySelector('#tunnel-name')
  if (nameInput) {
    const name = String(tunnelForm.frpProxyName || tunnelForm.name).trim()
    const invalid = provider === 'frp' && name && !/^[a-zA-Z0-9_-]{1,64}$/.test(name)
    nameInput.setCustomValidity(invalid ? t('app.frp.nameAsProxyInvalid') : '')
  }
  if (editor && !editor.reportValidity()) return
  if (provider === 'devtunnel' && tunnelForm.resourceError) {
    tunnelValidationError.value = tunnelForm.resourceError
    return
  }
  let selectedJumperIds = normalizeJumperIdList(tunnelForm.jumperIds)
  resetInlineJumperValidation()
  tunnelValidationError.value = ''

  try {
    if (provider === 'ssh' && tunnelForm.appendNewJumper) {
      const inlinePayload = buildJumperPayload(inlineJumperForm)
      const inlineError = validateJumperPayload(inlinePayload)
      if (inlineError) {
        inlineJumperValidationError.value = inlineError
        return
      }

      const createdJumper = await CreateJumper(inlinePayload)
      selectedJumperIds = [...selectedJumperIds, createdJumper.id]
      tunnelForm.jumperIds = selectedJumperIds
      logEvent('info', `Jumper ${createdJumper.name} created from New Tunnel`)
    }

    const editingStatus = editingTunnelId.value
      ? tunnels.value.find((item) => item.id === editingTunnelId.value)?.status || 'stopped'
      : 'stopped'
    const payload = buildTunnelPayload(editingStatus === 'busy' ? 'stopped' : editingStatus)
    if (provider === 'ssh') payload.jumperIds = selectedJumperIds
    const owner = provider === 'devtunnel'
      ? devTunnelServiceOwner(tunnels.value, payload.devTunnel?.tunnelId, editingTunnelId.value)
      : provider === 'cloudflare' ? cloudflareHostnameOwner(tunnels.value, payload.cloudflare?.hostname, editingTunnelId.value) : null
    if (owner) {
      tunnelValidationError.value = t(provider === 'devtunnel' ? 'app.modals.tunnel.duplicateService' : 'app.cloudflare.duplicateHostname', { name: owner.name })
      return
    }


    if (!payload.name || !payload.localHost || !payload.localPort) return
    if (nameUnits(payload.name) > TUNNEL_LIMITS.name) {
      tunnelValidationError.value = 'Tunnel name must be <= 20 chars or <= 10 Chinese chars.'
      return
    }
    if (provider === 'ssh' && !payload.jumperIds.length) return
    if (provider === 'ssh' && payload.mode !== 'dynamic' && (!payload.remoteHost || !payload.remotePort)) return
    if (provider === 'devtunnel' && !isValidTargetHost(payload.localHost)) {
      tunnelValidationError.value = t('app.modals.tunnel.invalidTargetHost')
      return
    }
    if (provider === 'devtunnel') {
      const expirationError = devTunnelExpirationError(tunnelForm.expirationAmount, tunnelForm.expirationUnit)
      if (expirationError) {
        tunnelValidationError.value = expirationError
        return
      }
      if (tunnelForm.tunnelIdMode === 'custom') {
        const nameError = devTunnelNameError(tunnelForm.devTunnelName)
        if (nameError) {
          tunnelValidationError.value = nameError
          return
        }
        const localOwner = devTunnelNameOwner(tunnels.value, normalizeDevTunnelName(tunnelForm.devTunnelName), editingTunnelId.value)
        if (localOwner) {
          tunnelValidationError.value = t('app.modals.tunnel.customIdOccupied', { name: localOwner.name })
          return
        }
        if (devTunnelNameTakenRemotely(normalizeDevTunnelName(tunnelForm.devTunnelName))) {
          tunnelValidationError.value = t('app.modals.tunnel.customIdRemoteConflict')
          return
        }
      }
    }
    if (provider === 'cloudflare') {
      if (!isValidTargetHost(payload.localHost)) {
        tunnelValidationError.value = t('app.modals.tunnel.invalidTargetHost')
        return
      }
      if (!isValidHostname(tunnelForm.hostname)) {
        tunnelValidationError.value = t('app.cloudflare.invalidHostname')
        return
      }
      if (tunnelForm.tunnelIdMode === 'bind' && !tunnelForm.tunnelId) {
        tunnelValidationError.value = t('app.cloudflare.tunnelIdRequired')
        return
      }
      if (tunnelForm.tunnelIdMode !== 'bind' && !String(tunnelForm.tunnelName || '').trim()) {
        tunnelValidationError.value = t('app.cloudflare.tunnelNameRequired')
        return
      }
    }

    if (editingTunnelId.value) {
      await UpdateTunnel(editingTunnelId.value, payload)
      logEvent('info', `Tunnel ${payload.name} updated`)
    } else {
      await CreateTunnel(payload)
      logEvent('info', `Tunnel ${payload.name} created`)
    }

    await loadStateFromBackend()
    showTunnelModal.value = false
  } catch (err) {
    tunnelValidationError.value = errorMessage(err)
  }
}

async function toggleTunnel(tunnel) {
  if (!tunnel || tunnel.status === 'busy' || pendingTunnelActions.has(tunnel.id)) {
    return
  }

  const shouldStart = tunnel.status === 'stopped' || tunnel.status === 'error'
  const runningLimit = proLicense.isPro ? 0 : FREE_PLAN_RUNNING_LIMIT
  const pendingStartCount = [...pendingTunnelActions.values()].filter((action) => action === 'start').length
  if (runningLimit > 0 && tunnel.status !== 'running' && Number(activeSlots.value) + pendingStartCount >= runningLimit) {
    const message = t('config.messages.freePlanRunningLimit', { limit: runningLimit })
    openActionDialog({
      mode: 'confirm',
      message,
      confirmButtonClass: 'btn-primary',
      confirmLabel: t('app.sidebar.upgrade'),
      onConfirm: async () => {
        await openProUpgrade()
      },
    })
    logEvent('warn', message)
    return
  }

  const previousStatus = tunnel.status
  const previousError = tunnel.lastError || ''
  pendingTunnelActions.set(tunnel.id, shouldStart ? 'start' : 'stop')
  patchTunnelLocal(tunnel.id, { status: 'busy', lastError: '' })

  // 记录启动开始时间
  const startTime = Date.now()

  try {
    const updated = await ToggleTunnel(tunnel.id)
    patchTunnelLocal(updated.id, updated)

    if (updated.status === 'error') {
      // A name conflict carries a stable backend marker; show a localized
      // explanation instead of the raw CLI text.
      if (updated.lastError && isDevTunnelNameConflict(updated.lastError)) {
        updated.lastError = t('app.modals.tunnel.customIdRemoteConflict')
        patchTunnelLocal(updated.id, updated)
      }
      const reason = updated.lastError ? `: ${updated.lastError}` : ''
      logEvent('error', `Tunnel ${updated.name} failed${reason}`)
      return
    }

    if (updated.status === 'busy') {
      logEvent('info', `Tunnel ${updated.name} starting`)
      return
    }

    if (previousStatus === 'error') {
      logEvent('info', `Tunnel ${tunnel.name} retry triggered`)
      return
    }

    const action = updated.status === 'running' ? 'started' : 'stopped'
    trackTunnelAction(action, updated.name)
    if (updated.status === 'running') {
      // 计算启动耗时
      const duration = Date.now() - startTime
      const durationText = duration < 1000 ? `${duration}ms` : `${(duration / 1000).toFixed(2)}s`
      logEvent('info', `Tunnel ${updated.name} started (took ${durationText})`)
    } else {
      logEvent('warn', `Tunnel ${updated.name} ${action}`)
    }
  } catch (err) {
    patchTunnelLocal(tunnel.id, { status: previousStatus, lastError: previousError })
    logEvent('error', errorMessage(err, `Failed to toggle tunnel ${tunnel.name}`))
  } finally {
    tunnelActionRevision++
    pendingTunnelActions.delete(tunnel.id)
  }
}

function openActionDialog({
  mode = 'alert',
  message,
  confirmButtonClass = 'btn-primary',
  confirmLabel = '',
  onConfirm = null,
  secondaryLabel = '',
  secondaryButtonClass = 'btn-outline-primary',
  onSecondary = null,
}) {
  actionDialog.previewLoading = false
  actionDialog.kind = ''
  actionDialog.mode = mode
  actionDialog.message = message
  actionDialog.confirmButtonClass = confirmButtonClass
  actionDialog.confirmLabel = confirmLabel
  actionDialog.onConfirm = onConfirm
  actionDialog.secondaryLabel = secondaryLabel
  actionDialog.secondaryButtonClass = secondaryButtonClass
  actionDialog.onSecondary = onSecondary
  actionDialog.busy = false
  actionDialog.error = ''
  actionDialog.preview = null
  actionDialog.lastResult = null
  actionDialog.visible = true
}

function closeActionDialog() {
  if (actionDialog.busy) return
  actionDialog.visible = false
  actionDialog.kind = ''
  actionDialog.confirmLabel = ''
  actionDialog.onConfirm = null
  actionDialog.secondaryLabel = ''
  actionDialog.onSecondary = null
  actionDialog.error = ''
  actionDialog.preview = null
  actionDialog.lastResult = null
}

async function confirmActionDialog() {
  if (actionDialog.busy) return
  if (actionDialog.kind === 'service-delete') {
    await runServiceDelete(actionDialog.cleanup && !!actionDialog.preview?.cleanupAvailable)
    return
  }
  const handler = actionDialog.onConfirm
  closeActionDialog()
  if (typeof handler === 'function') {
    await handler()
  }
}

async function secondaryActionDialog() {
  const handler = actionDialog.onSecondary
  closeActionDialog()
  if (typeof handler === 'function') {
    await handler()
  }
}

function openRedeemDialog(tab = 'purchase') {
  redeemDialog.initialTab = tab === 'activate' ? 'activate' : 'purchase'
  redeemDialog.visible = true
  redeemDialog.code = ''
  redeemDialog.error = ''
  redeemDialog.submitting = false
}

function closeRedeemDialog(force = false) {
  if (redeemDialog.submitting && !force) return
  redeemDialog.visible = false
  redeemDialog.code = ''
  redeemDialog.error = ''
}

async function submitRedeemDialog() {
  if (redeemDialog.submitting) return
  const code = String(redeemDialog.code || '').trim()
  if (!code) {
    redeemDialog.error = t('config.messages.enterLicenseCode')
    return
  }

  redeemDialog.error = ''
  redeemDialog.submitting = true
  licenseStateRevision++
  try {
    const redeemResult = await RedeemLicenseCodeAPI(code)
    if (!redeemResult?.success) {
      throw new Error(redeemResult?.message || t('purchase.inactiveCode'))
    }
    if (!redeemResult.active) throw new Error(t('purchase.inactiveCode'))
    licenseStateRevision++
    applyLicenseState({
      active: redeemResult?.active,
      expire_time: redeemResult?.expire_time,
      is_lifetime: redeemResult?.added_days >= LIFETIME_DURATION_DAYS,
      code: redeemResult?.code || code,
    })
    configMessage.value = t('config.messages.licenseRedeemedWithExpiry', { expiry: proExpiryLabel.value })
    logEvent('info', `License redeemed successfully (${proExpiryLabel.value})`)
    closeRedeemDialog(true)
    void refreshLicenseStatus({ silent: true })
  } catch (err) {
    const message = errorMessage(err, 'Failed to redeem license code.')
    redeemDialog.error = message
    configMessage.value = message
    logEvent('error', message)
  } finally {
    redeemDialog.submitting = false
  }
}

function deleteTunnel(tunnel) {
  trackTunnelAction('delete', tunnel.name)
  const provider = tunnel.provider || 'ssh'
  if (provider === 'ssh') {
    openActionDialog({
      mode: 'confirm',
      message: t('app.confirmations.deleteTunnel', { name: tunnel.name }),
      confirmButtonClass: 'btn-danger',
      onConfirm: async () => {
        try {
          await DeleteTunnel(tunnel.id)
          await loadStateFromBackend()
          logEvent('warn', `Tunnel ${tunnel.name} deleted`)
        } catch (err) {
          logEvent('error', errorMessage(err, `Failed to delete tunnel ${tunnel.name}`))
        }
      },
    })
    return
  }
  actionDialog.kind = 'service-delete'
  actionDialog.mode = 'confirm'
  actionDialog.message = ''
  actionDialog.confirmButtonClass = 'btn-danger'
  actionDialog.confirmLabel = ''
  actionDialog.onConfirm = null
  actionDialog.preview = { recordId: tunnel.id, name: tunnel.name, provider, cleanupAvailable: false }
  actionDialog.cleanup = false
  actionDialog.error = ''
  actionDialog.operationId = ''
  actionDialog.lastResult = null
  actionDialog.busy = true
  actionDialog.previewLoading = true
  actionDialog.visible = true
  PreviewTunnelDeletion(tunnel.id).then((preview) => {
    actionDialog.preview = { ...actionDialog.preview, ...preview, recordId: tunnel.id, name: preview?.name || tunnel.name }
    actionDialog.cleanup = !!preview?.cleanupAvailable
  }).catch((err) => {
    actionDialog.error = errorMessage(err)
    logEvent('error', actionDialog.error)
  }).finally(() => {
    actionDialog.previewLoading = false
    actionDialog.busy = false
  })
}

function deletionText(result) {
  const names = (result?.items || []).filter((item) => item.status === 'failed').map((item) => item.target).filter(Boolean).join(', ')
  const key = {
    in_progress: 'app.deletion.inProgress',
    stopped_cleanup_failed: 'app.deletion.stoppedCleanupFailed',
    port_unknown: 'app.deletion.portUnknown',
    dns_changed: 'app.deletion.dnsChanged',
    dns_permission: 'app.deletion.dnsPermission',
    dns_network: 'app.deletion.dnsNetwork',
    dns_failed: 'app.deletion.dnsNetwork',
    linked: 'app.deletion.linkedBlock',
    local_write_failed: 'app.deletion.stoppedCleanupFailed',
    tunnel_deleted_dns_partial: 'app.deletion.tunnelDeletedDnsPartial',
    remote_delete_failed: 'app.deletion.stoppedCleanupFailed',
    account_mismatch: 'app.deletion.dnsNetwork',
  }[result?.errorCategory]
  const text = key ? t(key, { names }) : t('app.providers.loadFailed')
  return names && key !== 'app.deletion.linkedBlock' ? `${text} ${names}` : text
}

async function runServiceDelete(cleanupRemote) {
  const preview = actionDialog.preview
  if (!preview?.recordId) return
  actionDialog.busy = true
  actionDialog.error = ''
  try {
    const result = actionDialog.operationId
      ? await RetryDeletionCleanup(actionDialog.operationId)
      : await DeleteTunnelWithOptions(preview.recordId, cleanupRemote)
    actionDialog.lastResult = result
    if (result?.status === 'completed') {
      await loadStateFromBackend()
      await refreshPendingDeletions()
      logEvent('warn', `Tunnel ${preview.name} deleted`)
      actionDialog.busy = false
      closeActionDialog()
      return
    }
    if (result?.detail) logEvent('error', result.detail)
    actionDialog.operationId = result?.operationId || actionDialog.operationId
    actionDialog.error = deletionText(result)
    await refreshPendingDeletions()
  } catch (err) {
    actionDialog.error = errorMessage(err)
    logEvent('error', actionDialog.error)
  } finally {
    actionDialog.busy = false
  }
}

async function deleteServiceLocalOnly() {
  actionDialog.operationId = ''
  actionDialog.cleanup = false
  await runServiceDelete(false)
}

async function refreshPendingDeletions() {
  try {
    pendingDeletions.value = (await ListDeletionTasks()) || []
  } catch {
    pendingDeletions.value = []
  }
}

async function retryPendingDeletion(operationID) {
  try {
    const result = await RetryDeletionCleanup(operationID)
    if (result?.detail) logEvent('error', result.detail)
    await loadStateFromBackend()
    await refreshPendingDeletions()
  } catch (err) {
    logEvent('error', errorMessage(err))
  }
}

function openTunnelGroupModal(groupId = null) {
  tunnelGroupModalError.value = ''
  pendingTunnelGroupEditId.value = groupId
  showTunnelGroupModal.value = true
  trackModalOpen('tunnel_group_manage', 'tunnels')
}

function closeTunnelGroupModal() {
  showTunnelGroupModal.value = false
  tunnelGroupModalError.value = ''
  pendingTunnelGroupEditId.value = null
}

async function createTunnelGroup(name) {
  try {
    tunnelGroupModalError.value = ''
    const trimmed = String(name || '').trim()
    if (!trimmed) {
      tunnelGroupModalError.value = t('app.tunnels.groups.nameRequired')
      return
    }
    if (nameUnits(trimmed) > TUNNEL_LIMITS.name) {
      tunnelGroupModalError.value = t('app.tunnels.groups.nameTooLong', {
        max: TUNNEL_LIMITS.name,
        half: Math.floor(TUNNEL_LIMITS.name / 2),
      })
      return
    }
    if (isTunnelGroupNameTaken(trimmed)) {
      tunnelGroupModalError.value = t('app.tunnels.groups.nameDuplicate')
      return
    }
    await CreateGroup({ name: trimmed })
    await loadStateFromBackend()
    logEvent('info', `Tunnel group ${name} created`)
  } catch (err) {
    const message = errorMessage(err, 'Failed to create tunnel group.')
    tunnelGroupModalError.value = /group name already exists/i.test(message)
      ? t('app.tunnels.groups.nameDuplicate')
      : message
  }
}

async function renameTunnelGroup({ id, name }) {
  try {
    tunnelGroupModalError.value = ''
    const trimmed = String(name || '').trim()
    if (!trimmed) {
      tunnelGroupModalError.value = t('app.tunnels.groups.nameRequired')
      return
    }
    if (nameUnits(trimmed) > TUNNEL_LIMITS.name) {
      tunnelGroupModalError.value = t('app.tunnels.groups.nameTooLong', {
        max: TUNNEL_LIMITS.name,
        half: Math.floor(TUNNEL_LIMITS.name / 2),
      })
      return
    }
    if (isTunnelGroupNameTaken(trimmed, id)) {
      tunnelGroupModalError.value = t('app.tunnels.groups.nameDuplicate')
      return
    }
    await UpdateGroup(id, { name: trimmed })
    await loadStateFromBackend()
    logEvent('info', `Tunnel group ${name} updated`)
  } catch (err) {
    const message = errorMessage(err, 'Failed to rename tunnel group.')
    tunnelGroupModalError.value = /group name already exists/i.test(message)
      ? t('app.tunnels.groups.nameDuplicate')
      : message
  }
}

function requestRenameTunnelGroup(group) {
  openTunnelGroupModal(group?.id ?? null)
}

function deleteTunnelGroup(group) {
  openActionDialog({
    mode: 'confirm',
    message: t('app.confirmations.deleteTunnelGroup', { name: group.name }),
    confirmButtonClass: 'btn-danger',
    onConfirm: async () => {
      try {
        tunnelGroupModalError.value = ''
        await DeleteGroup(group.id)
        await loadStateFromBackend()
        logEvent('warn', `Tunnel group ${group.name} deleted`)
      } catch (err) {
        const message = errorMessage(err, `Failed to delete tunnel group ${group.name}`)
        tunnelGroupModalError.value = message
        logEvent('error', message)
      }
    },
  })
}

async function reorderTunnelGroups(ids) {
  const orderedIds = (Array.isArray(ids) ? ids : [])
    .map((id) => Number(id))
    .filter((id) => Number.isInteger(id) && id > 0)
  if (orderedIds.length === 0) return

  const currentIds = tunnelGroups.value.map((group) => Number(group.id))
  if (orderedIds.length === currentIds.length && orderedIds.every((id, index) => id === currentIds[index])) {
    return
  }

  try {
    tunnelGroupModalError.value = ''
    await ReorderGroups(orderedIds)
    await loadStateFromBackend()
    logEvent('info', 'Tunnel groups reordered')
  } catch (err) {
    const message = errorMessage(err, 'Failed to reorder tunnel groups.')
    tunnelGroupModalError.value = message
    logEvent('error', message)
    await loadStateFromBackend()
  }
}

async function moveTunnelToGroup({ tunnel, groupId }) {
  if (!tunnel?.id) return
  const normalizedGroupId = Number(groupId) || 0
  if (normalizedGroupId === (Number(tunnel.groupId) || 0)) return
  if (tunnel.status === 'busy') return

  const groupName =
    normalizedGroupId === 0
      ? t('app.tunnels.groups.ungrouped')
      : tunnelGroups.value.find((group) => Number(group.id) === normalizedGroupId)?.name || String(normalizedGroupId)

  try {
    await MoveTunnelToGroup(tunnel.id, normalizedGroupId)
    await loadStateFromBackend()
    setConfigMessage(t('app.tunnels.groups.moved', { name: tunnel.name, group: groupName }))
    logEvent('info', `Tunnel ${tunnel.name} moved to group ${groupName}`)
  } catch (err) {
    const message = errorMessage(err, `Failed to move tunnel ${tunnel.name}`)
    setConfigMessage(message)
    logEvent('error', message)
  }
}

function deleteJumper(jumper) {
  trackJumperAction('delete', jumper.name)
  const inUseBy = tunnels.value.filter((item) => normalizeJumperIdList(item.jumperIds).includes(jumper.id))
  if (inUseBy.length > 0) {
    openActionDialog({
      mode: 'alert',
      message: t('app.confirmations.deleteJumperBlocked', { name: jumper.name, count: inUseBy.length }),
      confirmButtonClass: 'btn-primary',
    })
    logEvent('warn', `Delete blocked for jumper ${jumper.name} (still in use)`)
    return
  }
  openActionDialog({
    mode: 'confirm',
    message: t('app.confirmations.deleteJumper', { name: jumper.name }),
    confirmButtonClass: 'btn-danger',
    onConfirm: async () => {
      try {
        await DeleteJumper(jumper.id)
        await loadStateFromBackend()
        logEvent('warn', `Jumper ${jumper.name} deleted`)
      } catch (err) {
        logEvent('error', errorMessage(err, `Failed to delete jumper ${jumper.name}`))
      }
    },
  })
}

onMounted(async () => {
  void refreshFRP().catch(err => logEvent('error', String(err)))
  stopCloudStatus = EventsOn('cloud-sync:status', (status) => {
    cloudSyncStatus.value = status
  })
  stopCloudApplied = EventsOn('cloud-sync:applied', () => {
    void loadStateFromBackend({ silent: true })
  })
  stopDevTunnelLogin = EventsOn('devtunnel:login', () => {
    void finishDevTunnelLogin()
  })
  stopCloudflareLogin = EventsOn('cloudflare:login', () => {
    void finishCloudflareLogin()
  })
  stopCloudflareLoginProgress = EventsOn('cloudflare:login-progress', (payload) => {
    const phase = payload?.phase || ''
    const url = payload?.url || ''
    if (!String(cloudflareBusy.value).startsWith('login')) return
    cloudflareStatus.value = {
      ...(cloudflareStatus.value || {}),
      loginInProgress: true,
      loginPhase: phase || cloudflareStatus.value?.loginPhase || 'browser',
      loginUrl: url || cloudflareStatus.value?.loginUrl || '',
    }
  })
  stopDevTunnelLoginProgress = EventsOn('devtunnel:login-progress', (payload) => {
    const phase = payload?.phase || ''
    const url = payload?.url || ''
    if (!String(devTunnelBusy.value).startsWith('login')) return
    devTunnelStatus.value = {
      ...(devTunnelStatus.value || {}),
      loginInProgress: true,
      loginPhase: phase || devTunnelStatus.value?.loginPhase || 'browser',
      loginUrl: url || devTunnelStatus.value?.loginUrl || '',
    }
  })
  window.addEventListener('focus', cloudFocus)
  void GetCloudSyncStatus().then((status) => { cloudSyncStatus.value = status }).catch(() => {})
  const platform = typeof navigator !== 'undefined' ? navigator.platform || 'unknown' : 'unknown'
  trackAppStart(appMeta.version, platform)
  await loadStateFromBackend()
  void loadDevTunnelResources(false)
  stateSyncTimer = window.setInterval(syncStateSilently, STATE_SYNC_INTERVAL_MS)
  try {
    await SaveUILocale(locale.value)
  } catch (_) {
    /* tray locale sync is best-effort */
  }
  await loadStoredLicenseCode()
  try {
    const channel = String((await GetDistributionChannel()) || '')
      .trim()
      .toLowerCase()
    if (channel === 'store') distributionChannel.value = 'store'
  } catch (_) {
    // GitHub is the safe default for development/older bindings.
  }
  const licenseStatusReady = await refreshLicenseStatus({ silent: true })
  // If launch at login is on but user is not Pro or expired, disable it and show upgrade prompt
  try {
    const autoRunEnabled = await GetAutoRunEnabled()
    if (licenseStatusReady && autoRunEnabled && !proLicense.isPro) {
      configMessage.value = t('config.autoRunExpiredDisabled')
      await SetAutoRunEnabled(false)
    }
  } catch (err) {
    logEvent('error', errorMessage(err, 'Failed to check or reset launch-at-login (AutoRun).'))
  }
  try {
    trafficMonitorEnabled.value = await GetTrafficMonitorEnabled()
  } catch (_) {
    trafficMonitorEnabled.value = true
  }
  startTrafficSync()
  void checkForUpdatesSilently()
  void refreshPendingDeletions()
})

onBeforeUnmount(() => {
  stopCloudStatus?.()
  stopCloudApplied?.()
  stopDevTunnelLogin?.()
  stopDevTunnelLoginProgress?.()
  stopCloudflareLogin?.()
  stopCloudflareLoginProgress?.()
  stopDevTunnelLoginPoll()
  stopCloudflareLoginPoll()
  window.removeEventListener('focus', cloudFocus)
  if (stateSyncTimer !== null) {
    window.clearInterval(stateSyncTimer)
    stateSyncTimer = null
  }
  stopTrafficSync()
  if (configToastTimer !== null) {
    window.clearTimeout(configToastTimer)
    configToastTimer = null
  }
})

watch(
  () => jumperForm.authType,
  (newType) => {
    if (!authShowsPassword(newType)) jumperForm.password = ''
    if (!authNeedsKeyFile(newType)) jumperForm.keyPath = ''
    resetJumperTest()
  },
)

watch(
  () => inlineJumperForm.authType,
  (newType) => {
    if (!authShowsPassword(newType)) inlineJumperForm.password = ''
    if (!authNeedsKeyFile(newType)) inlineJumperForm.keyPath = ''
  },
)
function closeJumperModal() {
  showJumperModal.value = false
  resetAIDebugState(jumperAiDebug)
}

function closeTunnelModal() {
  frpDraftPending.value = false
  showTunnelModal.value = false
  resetAIDebugState(tunnelAiDebug)
}
</script>

<template>
  <div class="app-root">
    <div v-if="initialStateLoading" class="app-loading-overlay" role="status" aria-live="polite">
      <div class="app-loading-indicator">
        <span class="spinner-border text-primary" aria-hidden="true" />
        <span>{{ $t('app.loading') }}</span>
      </div>
    </div>
    <div class="app-shell" :inert="initialStateLoading ? '' : undefined" :aria-busy="initialStateLoading">
      <AppSidebar
        :pages="pages"
        :active-page="activePage"
        :app-version="appMeta.version"
        :is-pro="isPro"
        :pro-expiry-label="proExpiryLabel"
        :has-new-version="hasNewVersion"
        :collapsed="sidebarCollapsed"
        :traffic-monitor-enabled="trafficMonitorEnabled"
        :traffic="traffic"
        :traffic-history-up="trafficHistoryUp"
        :traffic-history-down="trafficHistoryDown"
        :show-upgrade-button="true"
        @switch-page="switchPage"
        @upgrade="openProUpgrade"
        @open-release-page="openReleasePage"
        @toggle-collapse="toggleSidebar"
      />

      <section class="content-shell">
        <AppTopHeader
          :class="{ 'providers-top-header': activePage === 'providers' }"
          :current-page="currentPage"
          :active-page="activePage"
          @new-tunnel="openNewTunnel"
          @import-tunnel="openImportTunnel"
        />

        <main class="page-body" :class="{ 'page-body-overview': activePage === 'overview' }">
          <div
            v-if="
              cloudSyncStatus?.enabled &&
              ['update_available', 'conflict', 'error'].includes(cloudSyncStatus.state) &&
              activePage === 'overview'
            "
            class="sync-banner"
            :class="`sync-banner--${cloudSyncStatus.state}`"
            role="status"
          >
            <div class="sync-banner-summary">
              <span class="sync-banner-lead">
                <i class="bi sync-banner-icon" :class="cloudSyncBannerIcons[cloudSyncStatus.state]" aria-hidden="true" />
                <span class="sync-banner-title">{{ t(`cloudSync.banner.${cloudSyncStatus.state}`) }}</span>
              </span>
              <span class="sync-banner-version">{{ t('cloudSync.banner.version', { revision: cloudSyncStatus.revision }) }}</span>
            </div>
            <button type="button" class="sync-banner-action" @click="switchPage('sync')">
              {{ t('cloudSync.review') }}
              <span class="sync-banner-arrow" aria-hidden="true">→</span>
            </button>
          </div>
          <div v-if="pendingDeletions.length" class="deletion-banner" role="status">
            <span>{{ t('app.deletion.pendingBanner') }}</span>
            <button type="button" class="btn btn-sm" @click="retryPendingDeletion(pendingDeletions[0].operationId)">
              {{ t('app.deletion.retryCleanup') }}
            </button>
          </div>
          <OverviewPage
            v-if="activePage === 'overview'"
            :tunnels="tunnels"
            :total-tunnels="totalTunnels"
            :running-tunnels="runningTunnels"
            :pending-tunnel-actions="pendingTunnelActions"
            :stopped-tunnels="stoppedTunnels"
            :auto-start-count="autoStartTunnels.length"
            :logs="logs"
            :get-tunnel-jumper-label="getTunnelJumperLabel"
            :describe-dev-tunnel="describeDevTunnel"
            :describe-cloudflare="describeCloudflareTunnel"
            @toggle-tunnel="toggleTunnel"
            @open-tunnels="switchPage('tunnels')"
          />

          <ProvidersPage
            v-if="activePage === 'providers'"
            :frp-draft-pending="frpDraftPending"
            @resume-frp-draft="resumeFRPDraft"
            @create-frp-tunnel="openNewFRPTunnel"
            @view-frp-tunnels="viewFRPTunnels"
            :tab="providerTab"
            :jumpers="filteredJumpers"
            :jumper-total="jumpers.length"
            :search-query="jumperSearchQuery"
            :dev-tunnel-status="devTunnelStatus"
            :dev-tunnel-busy="devTunnelBusy"
            :dev-tunnel-resources="devTunnelResources"
            :tunnels="tunnels"
            :dev-tunnel-resource-busy="devTunnelResourceBusy"
            :dev-tunnel-created="devTunnelCreated"
            :dev-tunnel-action-result="devTunnelActionResult"
            :dev-tunnel-detail="devTunnelDetail"
            :dev-tunnel-create-error="devTunnelCreateError"
            :dev-tunnel-region-probe="devTunnelRegionProbe"
            @update:tab="providerTab = $event"
            @update-search-query="jumperSearchQuery = $event"
            @import-jumper="openImportJumper"
            @new-jumper="openNewJumper"
            @copy-jumper="copyJumper"
            @edit-jumper="editJumper"
            @delete-jumper="deleteJumper"
            @refresh-devtunnel="checkDevTunnel"
            @install-devtunnel="installDevTunnel"
            @cancel-devtunnel-install="cancelDevTunnelInstall"
            @login-devtunnel="loginDevTunnel"
            @cancel-devtunnel-login="cancelDevTunnelLogin"
            @logout-devtunnel="logoutDevTunnel"
            @open-devtunnel-login="openDevTunnelLoginURL"
            @open-devtunnel-docs="openDevTunnelDocs"
            @refresh-devtunnel-resources="loadDevTunnelResources(true)"
            @create-devtunnel-resource="createDevTunnelResource"
            @delete-devtunnel-resource="deleteDevTunnelResourceByID"
            @retry-devtunnel-cleanup="retryProviderCleanup($event, 'devtunnel')"
            @create-devtunnel="openNewTunnelFromDevTunnelID"
            @inspect-devtunnel-resource="inspectDevTunnelResource"
            @open-url="openExternalUrl"
            @reset-devtunnel-created="devTunnelCreated = null; devTunnelCreateError = ''"
            @load-devtunnel-regions="loadDevTunnelRegions(false)"
            @test-devtunnel-regions="loadDevTunnelRegions(true)"
            :cloudflare-status="cloudflareStatus"
            :cloudflare-busy="cloudflareBusy"
            :cloudflare-resources="cloudflareResources"
            :cloudflare-resource-busy="cloudflareResourceBusy"
            :cloudflare-created="cloudflareCreated"
            :cloudflare-action-result="cloudflareActionResult"
            :cloudflare-detail="cloudflareDetail"
            :cloudflare-create-error="cloudflareCreateError"
            :cloudflare-zones="cloudflareZones"
            @refresh-cloudflare="checkCloudflare"
            @install-cloudflare="installCloudflare"
            @cancel-cloudflare-install="cancelCloudflareInstall"
            @login-cloudflare="loginCloudflare"
            @cancel-cloudflare-login="cancelCloudflareLogin"
            @open-cloudflare-login="openDevTunnelLoginURL"
            @open-cloudflare-docs="openCloudflareDocs"
            @refresh-cloudflare-resources="loadCloudflareResources(true)"
            @refresh-cloudflare-zones="loadCloudflareZones(true)"
            @cloudflare-authorizations-changed="cloudflareAuthorizationsChanged"
            @create-cloudflare-resource="createCloudflareResource"
            @delete-cloudflare-resource="deleteCloudflareResourceByID"
            @retry-cloudflare-cleanup="retryProviderCleanup($event, 'cloudflare')"
            @create-cloudflare="openNewTunnelFromCloudflareID"
            @inspect-cloudflare-resource="inspectCloudflareResource"
            @reset-cloudflare-created="cloudflareCreated = null; cloudflareCreateError = ''"
          />

          <TunnelsPage
            v-if="activePage === 'tunnels'"
            :tunnels="filteredTunnels"
            :pending-tunnel-actions="pendingTunnelActions"
            :groups="tunnelGroups"
            :hide-empty-ungrouped="hideEmptyUngrouped"
            :search-query="tunnelSearchQuery"
            :mode-options="modeOptions"
            :tunnel-ai-debug-states="tunnelErrorAiDebugStates"
            :ai-debug-enabled="AI_DEBUG_ENABLED"
            :get-tunnel-jumper-label="getTunnelJumperLabel"
            :describe-dev-tunnel="describeDevTunnel"
            :describe-cloudflare="describeCloudflareTunnel"
            @update-search-query="tunnelSearchQuery = $event"
            @toggle-tunnel="toggleTunnel"
            @copy-tunnel="copyTunnel"
            @edit-tunnel="editTunnel"
            @delete-tunnel="deleteTunnel"
            @ai-debug="openSavedTunnelAIDebug"
            @manage-groups="openTunnelGroupModal()"
            @open-url="BrowserOpenURL"
            @rename-group="requestRenameTunnelGroup"
            @delete-group="deleteTunnelGroup"
            @move-tunnel-to-group="moveTunnelToGroup"
            @open-provider="openTunnelProvider"
          />

          <LogsPage
            v-if="activePage === 'logs'"
            :selected-log-level="selectedLogLevel"
            :filtered-logs="filteredLogs"
            @set-log-level="selectedLogLevel = $event"
          />

          <SyncPage
            v-if="activePage === 'sync'"
            @reload-state="loadStateFromBackend"
            @confirm-action="openActionDialog"
          />

          <ConfigPage
            v-if="activePage === 'config'"
            :theme="theme"
            :accent-color="accentColor"
            :app-meta="appMeta"
            :is-store-distribution="isStoreDistribution"
            :is-pro="isPro"
            :pro-expiry-label="proExpiryLabel"
            :license-code="proLicense.code"
            :config-message="configMessage"
            :is-checking-updates="isCheckingUpdates"
            :is-refreshing-license-status="isRefreshingLicenseStatus"
            :update-check-dialog="updateCheckDialog"
            @theme-change="setThemeBySwitch"
            @accent-color-change="setAccentColor"
            @check-updates="checkForUpdates"
            @open-release-page="openReleasePage"
            @close-update-check-dialog="closeUpdateCheckDialog"
            @refresh-license-status="refreshLicenseStatusFromConfig"
            @upgrade="openProUpgrade"
            @set-config-message="setConfigMessage"
            @reload-state="loadStateFromBackend"
            @confirm-action="openActionDialog"
            @traffic-monitor-change="onTrafficMonitorChange"
          />
        </main>

        <div
          v-if="showConfigToast && configMessage"
          class="toast-container position-absolute bottom-0 start-50 translate-middle-x p-3 toast-config-container"
        >
          <div
            class="toast show align-items-center text-bg-dark border-0 toast-config-item"
            role="alert"
            aria-live="assertive"
            aria-atomic="true"
          >
            <div class="d-flex">
              <div class="toast-body">
                {{ configMessage }}
              </div>
              <button
                type="button"
                class="btn-close btn-close-white me-2 m-auto"
                :aria-label="$t('app.common.close')"
                @click="hideConfigToast"
              />
            </div>
          </div>
        </div>
      </section>
    </div>

    <Teleport to="body">
      <div v-if="actionDialog.visible" class="overlay action-confirm-overlay">
        <div class="dialog-card compact-dialog action-confirm-dialog" :class="{ 'service-delete-dialog': actionDialog.kind === 'service-delete' }" role="dialog" aria-modal="true" aria-labelledby="action-confirm-title" aria-describedby="action-confirm-message">
          <div class="dialog-head">
            <h3 id="action-confirm-title" class="dialog-title">
              {{ actionDialog.kind === 'service-delete' ? t('app.deletion.deleteServiceTitle', { name: actionDialog.preview?.name || '' }) : $t('app.common.confirm') }}
            </h3>
          </div>
          <div class="dialog-body">
            <template v-if="actionDialog.kind === 'service-delete'">
              <p v-if="actionDialog.previewLoading" class="service-delete-loading" role="status">
                <span class="spinner-border spinner-border-sm" aria-hidden="true" />
                {{ t('app.deletion.previewLoading') }}
              </p>
              <dl v-else-if="actionDialog.preview" class="service-delete-lines">
                <div>
                  <dt>{{ t('app.deletion.localService') }}</dt>
                  <dd>{{ actionDialog.preview.localService || '—' }}</dd>
                </div>
                <div v-if="actionDialog.preview.tunnelId">
                  <dt>{{ t('app.deletion.tunnelId') }}</dt>
                  <dd>{{ actionDialog.preview.tunnelId }}</dd>
                </div>
                <div v-if="actionDialog.preview.remotePort">
                  <dt>{{ t('app.deletion.remotePort') }}</dt>
                  <dd>{{ actionDialog.preview.remotePort }}</dd>
                </div>
                <div v-if="actionDialog.preview.hostname">
                  <dt>{{ t('app.deletion.hostname') }}</dt>
                  <dd>{{ actionDialog.preview.hostname }}</dd>
                </div>
              </dl>
              <label v-if="!actionDialog.previewLoading && actionDialog.preview?.cleanupAvailable" class="service-delete-check">
                <input v-model="actionDialog.cleanup" type="checkbox" :disabled="actionDialog.busy" />
                <span>{{ actionDialog.preview.provider === 'cloudflare' ? t('app.deletion.cleanupDns') : t('app.deletion.cleanupPort') }}</span>
              </label>
              <p v-if="!actionDialog.previewLoading && !actionDialog.error" class="service-delete-note">
                {{
                  actionDialog.preview?.cleanupAvailable && actionDialog.cleanup
                    ? (actionDialog.preview.provider === 'cloudflare' ? t('app.deletion.dnsOnlyMatch') + ' ' + t('app.deletion.keepIdShort') : t('app.deletion.keepId'))
                    : t('app.deletion.localOnly')
                }}
              </p>
              <p v-if="actionDialog.error" class="form-error mb-0 mt-2">{{ actionDialog.error }}</p>
            </template>
            <p v-else id="action-confirm-message" class="action-dialog-message mb-0">{{ actionDialog.message }}</p>
          </div>
          <div class="dialog-footer">
            <button
              v-if="actionDialog.mode === 'confirm'"
              type="button"
              class="btn btn-outline-secondary"
              :disabled="actionDialog.busy"
              @click="closeActionDialog"
            >
              {{ $t('app.common.cancel') }}
            </button>
            <button
              v-if="actionDialog.kind === 'service-delete' && actionDialog.error && !actionDialog.lastResult?.localDeleted"
              type="button"
              class="btn btn-outline-secondary"
              :disabled="actionDialog.busy"
              @click="deleteServiceLocalOnly"
            >
              {{ t('app.deletion.localFallback') }}
            </button>
            <button
              v-if="actionDialog.onSecondary"
              type="button"
              class="btn"
              :class="actionDialog.secondaryButtonClass"
              :disabled="actionDialog.busy"
              @click="secondaryActionDialog"
            >
              {{ actionDialog.secondaryLabel }}
            </button>
            <button type="button" class="btn" :class="actionDialog.confirmButtonClass" :disabled="actionDialog.busy || (actionDialog.kind === 'service-delete' && !actionDialog.preview)" @click="confirmActionDialog">
              {{
                actionDialog.busy && actionDialog.kind === 'service-delete'
                  ? (actionDialog.previewLoading ? t('app.deletion.previewLoadingShort') : t('app.deletion.deleting'))
                  : actionDialog.kind === 'service-delete'
                    ? (actionDialog.error ? t('app.deletion.retry') : t('app.deletion.deleteService'))
                    : (actionDialog.confirmLabel || (actionDialog.mode === 'confirm' ? $t('app.common.delete') : $t('app.common.confirm')))
              }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <AIDebugModal
      v-if="AI_DEBUG_ENABLED"
      :show="!!selectedAIDebugTunnel"
      :title="selectedAIDebugTunnelTitle"
      :subtitle="selectedAIDebugTunnelSubtitle"
      :raw-error="selectedAIDebugTunnel?.lastError || ''"
      :state="selectedAIDebugTunnelState"
      @close="closeSavedTunnelAIDebug"
      @retry-debug="retrySavedTunnelAIDebug"
      @test-again="retestSavedTunnelFromAIDebug"
      @report-content="reportAIDebugContent('saved_tunnel', selectedAIDebugTunnelState)"
    />

    <ProPurchaseDialog
      v-if="redeemDialog.visible"
      v-model:code="redeemDialog.code"
      :initial-tab="redeemDialog.initialTab"
      :error="redeemDialog.error"
      :submitting="redeemDialog.submitting"
      @close="closeRedeemDialog()"
      @submit="submitRedeemDialog"
      @purchase="openExternalUrl"
    />

    <JumperModal
      :show="showJumperModal"
      :editing-jumper-id="editingJumperId"
      :jumper-form="jumperForm"
      :show-jumper-basic="showJumperBasic"
      :show-jumper-advanced="showJumperAdvanced"
      :auth-options="authOptions"
      :jumper-needs-key-file="jumperNeedsKeyFile"
      :jumper-needs-password="jumperNeedsPassword"
      :jumper-shows-password="jumperShowsPassword"
      :jumper-limits="JUMPER_LIMITS"
      :jumper-validation-error="jumperValidationError"
      :jumper-test="jumperTest"
      :jumper-ai-debug="jumperAiDebug"
      :ai-debug-enabled="AI_DEBUG_ENABLED"
      @close="closeJumperModal"
      @submit="saveJumper"
      @toggle-basic="showJumperBasic = !showJumperBasic"
      @toggle-advanced="showJumperAdvanced = !showJumperAdvanced"
      @key-file-change="onJumperKeyFileChange"
      @test-connection="testJumperConnection"
      @ai-debug="runJumperAIDebug"
      @report-ai-content="reportAIDebugContent('jumper', jumperAiDebug)"
    />

    <ImportJumperModal
      :show="showImportJumperModal"
      :candidates="sshConfigCandidates"
      :existing-jumpers="jumpers"
      :auth-options="authOptions"
      :jumper-limits="JUMPER_LIMITS"
      :sources="sshConfigSources"
      :selected-source-path="selectedImportJumperSourcePath"
      :loading="importJumperLoading"
      :load-error="importJumperError"
      :import-error="importJumperError"
      :has-loaded="importJumperHasLoaded"
      @close="closeImportJumper"
      @update:selected-source-path="selectedImportJumperSourcePath = $event"
      @load="loadImportJumpers"
      @import="importJumpers"
    />

    <TunnelModal
      :show="showTunnelModal"
      :editing-tunnel-id="editingTunnelId"
      :tunnel-form="tunnelForm"
      :tunnels="tunnels"
      :groups="tunnelGroups"
      :mode-options="tunnelDialogModeOptions"
      :jumpers="jumpers"
      :inline-jumper-form="inlineJumperForm"
      :auth-options="authOptions"
      :inline-jumper-needs-key-file="inlineJumperNeedsKeyFile"
      :inline-jumper-needs-password="inlineJumperNeedsPassword"
      :inline-jumper-shows-password="inlineJumperShowsPassword"
      :jumper-limits="JUMPER_LIMITS"
      :inline-jumper-validation-error="inlineJumperValidationError"
      :tunnel-validation-error="tunnelValidationError"
      :tunnel-test="tunnelTest"
      :tunnel-ai-debug="tunnelAiDebug"
      :ai-debug-enabled="AI_DEBUG_ENABLED"
      :dev-tunnel-resources="devTunnelResources"
      :dev-tunnel-account-state="devTunnelStatus.accountState || ''"
      :dev-tunnel-region-probe="devTunnelRegionProbe"
      :cloudflare-resources="cloudflareResources"
      :cloudflare-account-state="cloudflareStatus.accountState || ''"
      :cloudflare-zones="cloudflareZones"
      :editing-tunnel-status="editingTunnelStatus"
      @close="closeTunnelModal"
      @manage-frp="manageFRP"
      @manage-devtunnel-ids="manageDevTunnelIDs"
      @refresh-devtunnel-resources="loadDevTunnelResources(true)"
      @select-devtunnel-id="inspectDevTunnelID"
      @test-devtunnel-regions="loadDevTunnelRegions(true)"
      @manage-cloudflare-ids="manageCloudflareIDs"
      @refresh-cloudflare-resources="loadCloudflareResources(true)"
      @refresh-cloudflare-zones="loadCloudflareZones(true)"
      @select-cloudflare-id="inspectCloudflareID"
      @open-url="openExternalUrl"
      @submit="saveTunnel"
      @provider-change="onTunnelProviderChange"
      @set-primary-jumper="setPrimaryJumperForTunnelChain"
      @add-jumper="addJumperToTunnelChain"
      @move-jumper="moveJumperInTunnelChain"
      @trim-jumpers-to-primary="trimJumperChainToPrimary"
      @remove-jumper="removeJumperFromTunnelChain"
      @inline-key-file-change="onInlineJumperKeyFileChange"
      @test-connection="testTunnelConnection"
      @ai-debug="runTunnelAIDebug"
      @report-ai-content="reportAIDebugContent('tunnel', tunnelAiDebug)"
    />

    <TunnelGroupModal
      :show="showTunnelGroupModal"
      :groups="tunnelGroups"
      :hide-empty-ungrouped="hideEmptyUngrouped"
      :initial-edit-group-id="pendingTunnelGroupEditId"
      :error-message="tunnelGroupModalError"
      :name-max-length="TUNNEL_LIMITS.name"
      @close="closeTunnelGroupModal"
      @create-group="createTunnelGroup"
      @rename-group="renameTunnelGroup"
      @delete-group="deleteTunnelGroup"
      @reorder-groups="reorderTunnelGroups"
      @update:hide-empty-ungrouped="setHideEmptyUngrouped"
    />

    <ImportTunnelModal
      :show="showImportTunnelModal"
      :jumpers="jumpers"
      :existing-tunnels="tunnels"
      :mode-options="modeOptions"
      :auth-options="authOptions"
      :jumper-limits="JUMPER_LIMITS"
      :import-error="importTunnelError"
      @close="closeImportTunnel"
      @import="importTunnels"
      @frpc-imported="loadStateFromBackend"
    />
  </div>
</template>
