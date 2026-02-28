<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CheckForUpdates as CheckForUpdatesAPI,
  CreateJumper,
  CreateTunnel,
  DeleteJumper,
  DeleteTunnel,
  GetMachineID,
  GetState,
  TestJumperConnection as TestJumperConnectionAPI,
  TestTunnelConnection as TestTunnelConnectionAPI,
  ToggleTunnel,
  UpdateJumper,
  UpdateTunnel
} from '../wailsjs/go/main/App'
import { BrowserOpenURL } from '../wailsjs/runtime/runtime'
import AppSidebar from './components/layout/AppSidebar.vue'
import AppTopHeader from './components/layout/AppTopHeader.vue'
import OverviewPage from './components/pages/OverviewPage.vue'
import JumpersPage from './components/pages/JumpersPage.vue'
import TunnelsPage from './components/pages/TunnelsPage.vue'
import LogsPage from './components/pages/LogsPage.vue'
import ConfigPage from './components/pages/ConfigPage.vue'
import JumperModal from './components/modals/JumperModal.vue'
import TunnelModal from './components/modals/TunnelModal.vue'
import ImportTunnelModal from './components/modals/ImportTunnelModal.vue'
import { getLicenseStatus, redeemLicenseCode } from './utils/backend-api'
import './styles/app-shell.css'

const { t, locale } = useI18n()

const pages = computed(() => [
  { key: 'overview', title: t('app.sidebar.overview'), subtitle: t('app.sidebar.overviewSubtitle') },
  { key: 'jumpers', title: t('app.sidebar.jumpers'), subtitle: t('app.sidebar.jumpersSubtitle') },
  { key: 'tunnels', title: t('app.sidebar.tunnels'), subtitle: t('app.sidebar.tunnelsSubtitle') },
  { key: 'logs', title: t('app.sidebar.logs'), subtitle: t('app.sidebar.logsSubtitle') },
  { key: 'config', title: t('app.sidebar.config'), subtitle: t('app.sidebar.configSubtitle') }
])

const modeOptions = computed(() => [
  { value: 'local', label: t('app.options.mode.local') },
  { value: 'remote', label: t('app.options.mode.remote') },
  { value: 'dynamic', label: t('app.options.mode.dynamic') }
])

const authOptions = computed(() => [
  { value: 'password', label: t('app.options.auth.password') },
  { value: 'ssh_key', label: t('app.options.auth.sshKey') },
  { value: 'ssh_agent', label: t('app.options.auth.sshAgent') }
])

const savedTheme = typeof window !== 'undefined' ? window.localStorage.getItem('lt.theme') : null
const theme = ref(savedTheme === 'dark' ? 'dark' : 'light')
const activePage = ref('overview')
const selectedLogLevel = ref('all')
const configMessage = ref('')
const showReleasePageButton = ref(false)
const DEFAULT_RELEASES_PAGE_URL = 'https://github.com/RangerWolf/loris-tunnel-app/releases'
const releasePageUrl = ref(DEFAULT_RELEASES_PAGE_URL)
const showOverviewActive = ref(true)
const showOverviewActivity = ref(true)

const appMeta = reactive({
  version: '0.15.13-alpha',
  channel: 'Community',
  updater: 'GitHub Releases API (via Go backend)',
  build: '2026-02-25'
})
const proLicense = reactive({
  isPro: false,
  expiresAt: '',
  isLifetime: false
})
const LIFETIME_DURATION_DAYS = 36500
const machineId = ref('')

watchEffect(() => {
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-theme', theme.value)
    document.documentElement.setAttribute('data-bs-theme', theme.value)
  }
})

watch(theme, (newTheme) => {
  if (typeof window !== 'undefined') {
    window.localStorage.setItem('lt.theme', newTheme)
  }
})

const jumpers = ref([])
const tunnels = ref([])
const jumperSearchQuery = ref('')
const tunnelSearchQuery = ref('')
const STATE_SYNC_INTERVAL_MS = 5000
const pendingToggleTunnelIds = new Set()
let stateSyncTimer = null
let stateSyncInFlight = false

const logs = ref([
  { id: 1, level: 'info', time: nowLabel(), message: 'Config storage mode: TOML' }
])

const showJumperModal = ref(false)
const showTunnelModal = ref(false)
const showImportTunnelModal = ref(false)
const importTunnelError = ref('')
const editingJumperId = ref(null)
const editingTunnelId = ref(null)
const showJumperBasic = ref(true)
const showJumperAdvanced = ref(false)

const jumperForm = reactive(defaultJumperForm())
const tunnelForm = reactive(defaultTunnelForm())
const inlineJumperForm = reactive(defaultInlineJumperForm())

const jumperValidationError = ref('')
const inlineJumperValidationError = ref('')
const tunnelValidationError = ref('')
const actionDialog = reactive({
  visible: false,
  mode: 'alert',
  message: '',
  confirmButtonClass: 'btn-primary',
  confirmLabel: '',
  onConfirm: null
})
const redeemDialog = reactive({
  visible: false,
  code: '',
  error: '',
  submitting: false
})
const jumperTest = reactive({
  status: 'idle',
  message: ''
})
const tunnelTest = reactive({
  status: 'idle',
  message: ''
})

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
  timeoutMax: 120000
}
const TUNNEL_LIMITS = {
  name: 20
}
const FREE_PLAN_RUNNING_LIMIT = 3

const currentPage = computed(() => pages.value.find((page) => page.key === activePage.value))
const totalTunnels = computed(() => tunnels.value.length)
const runningTunnels = computed(() => tunnels.value.filter((tunnel) => tunnel.status === 'running'))
const stoppedTunnels = computed(() => tunnels.value.filter((tunnel) => tunnel.status === 'stopped' || tunnel.status === 'error'))
const autoStartTunnels = computed(() => tunnels.value.filter((tunnel) => tunnel.autoStart))
const filteredLogs = computed(() => {
  if (selectedLogLevel.value === 'all') return logs.value
  return logs.value.filter((log) => log.level === selectedLogLevel.value)
})

const filteredJumpers = computed(() => {
  const query = jumperSearchQuery.value.trim().toLowerCase()
  if (!query) return jumpers.value
  
  return jumpers.value.filter(jumper => {
    return (
      jumper.name.toLowerCase().includes(query) ||
      jumper.host.toLowerCase().includes(query) ||
      jumper.user.toLowerCase().includes(query) ||
      (jumper.notes && jumper.notes.toLowerCase().includes(query))
    )
  })
})

const filteredTunnels = computed(() => {
  const query = tunnelSearchQuery.value.trim().toLowerCase()
  if (!query) return tunnels.value
  
  return tunnels.value.filter(tunnel => {
    const jumperName = getTunnelJumperLabel(tunnel).toLowerCase()
    return (
      tunnel.name.toLowerCase().includes(query) ||
      tunnel.localHost.toLowerCase().includes(query) ||
      tunnel.remoteHost.toLowerCase().includes(query) ||
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
  if (proLicense.isLifetime) return locale.value === 'zh-CN' ? '终身' : 'Lifetime'
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
    notes: ''
  }
}

function defaultTunnelForm() {
  return {
    name: '',
    mode: 'local',
    jumperIds: [],
    nextJumperId: '',
    appendNewJumper: false,
    localHost: '127.0.0.1',
    localPort: 10022,
    remoteHost: '',
    remotePort: 22,
    autoStart: false,
    description: ''
  }
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
    notes: ''
  }
}

function nowLabel() {
  return new Date().toLocaleString()
}

async function ensureMachineId() {
  if (machineId.value) return machineId.value
  const id = String(await GetMachineID() || '').trim()
  if (!id) {
    throw new Error('Failed to get machine ID from Wails backend.')
  }
  machineId.value = id
  return id
}

function formatDateTime(value) {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return String(value)
  return date.toLocaleDateString()
}

function applyLicenseState({ active = false, expire_time = null, is_lifetime = false }) {
  proLicense.isPro = !!active
  proLicense.expiresAt = expire_time ? String(expire_time) : ''
  proLicense.isLifetime = !!is_lifetime
  appMeta.channel = proLicense.isPro ? 'Pro' : 'Community'
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

function nameUnits(text) {
  let units = 0
  for (const char of text || '') {
    units += /[\u3400-\u9fff\uf900-\ufaff]/.test(char) ? 2 : 1
  }
  return units
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
  return String(host || '').trim().toLowerCase()
}

function getTunnelImportSignature(tunnelLike) {
  const mode = String(tunnelLike?.mode || 'local').trim().toLowerCase()
  const localHost = toHostKey(tunnelLike?.localHost || '127.0.0.1')
  const localPort = Number(tunnelLike?.localPort) || 0
  const remoteHost = mode === 'dynamic' ? '' : toHostKey(tunnelLike?.remoteHost)
  const remotePort = mode === 'dynamic' ? 0 : (Number(tunnelLike?.remotePort) || 0)
  const jumperKey = normalizeJumperIdList(tunnelLike?.jumperIds).join(',')
  return `${mode}|${localHost}|${localPort}|${remoteHost}|${remotePort}|${jumperKey}`
}

function getNextTunnelJumperCandidate(selectedIds = []) {
  if (!jumpers.value.length) return ''
  const selected = new Set(normalizeJumperIdList(selectedIds))
  const candidate = jumpers.value.find((item) => !selected.has(item.id))
  return candidate ? candidate.id : jumpers.value[0].id
}

function getTunnelJumperLabel(tunnel) {
  const ids = normalizeJumperIdList(tunnel?.jumperIds)
  if (!ids.length) return t('app.options.jumper.unknown')
  const names = ids.map((id) => getJumperName(id))
  return names.join(' -> ')
}

function normalizeTunnelFromBackend(tunnel) {
  const rawLatency = Number(tunnel?.latencyMs)
  return {
    ...tunnel,
    jumperIds: normalizeJumperIdList(tunnel?.jumperIds),
    latencyMs: Number.isFinite(rawLatency) && rawLatency > 0 ? rawLatency : 0
  }
}

function logEvent(level, message) {
  logs.value.unshift({
    id: nextId(logs.value),
    level,
    time: nowLabel(),
    message
  })
}

function errorMessage(err, fallback = 'Operation failed.') {
  if (!err) return fallback
  if (typeof err === 'string') return err
  if (typeof err.message === 'string' && err.message) return err.message
  return fallback
}

async function loadStateFromBackend(options = {}) {
  const { silent = false } = options
  try {
    const state = await GetState()
    jumpers.value = Array.isArray(state?.jumpers) ? state.jumpers : []
    const backendTunnels = (Array.isArray(state?.tunnels) ? state.tunnels : []).map(normalizeTunnelFromBackend)

    if (pendingToggleTunnelIds.size === 0) {
      tunnels.value = backendTunnels
      return
    }

    const localBusyTunnelIds = new Set(
      tunnels.value
        .filter((item) => pendingToggleTunnelIds.has(item.id) && item.status === 'busy')
        .map((item) => item.id)
    )

    tunnels.value = backendTunnels.map((item) => {
      if (!localBusyTunnelIds.has(item.id)) return item
      return { ...item, status: 'busy', lastError: '', latencyMs: 0 }
    })
  } catch (err) {
    if (silent) return
    const message = errorMessage(err, 'Failed to load config from backend.')
    configMessage.value = message
    logEvent('error', message)
  }
}

function syncStateSilently() {
  if (stateSyncInFlight) return
  stateSyncInFlight = true
  loadStateFromBackend({ silent: true }).finally(() => {
    stateSyncInFlight = false
  })
}

function switchPage(pageKey) {
  activePage.value = pageKey
}

function setThemeBySwitch(enabled) {
  theme.value = enabled ? 'dark' : 'light'
  logEvent('info', `Theme switched to ${theme.value}`)
}

async function refreshLicenseStatus(options = {}) {
  const { silent = false } = options
  try {
    const id = await ensureMachineId()
    const status = await getLicenseStatus(id)
    applyLicenseState(status || {})
    if (!silent) {
      if (proLicense.isPro) {
        configMessage.value = `License is active (${proExpiryLabel.value}).`
        logEvent('info', `License status refreshed: active (${proExpiryLabel.value})`)
      } else {
        configMessage.value = 'License is not activated.'
        logEvent('info', 'License status refreshed: inactive')
      }
    }
  } catch (err) {
    if (!silent) {
      const message = errorMessage(err, 'Failed to query license status from backend API.')
      configMessage.value = message
      logEvent('error', message)
    }
  }
}

async function checkForUpdates() {
  try {
    const result = await CheckForUpdatesAPI(appMeta.version)

    if (!result?.hasUpdate) {
      showReleasePageButton.value = false
      releasePageUrl.value = DEFAULT_RELEASES_PAGE_URL
      configMessage.value = `Checked at ${nowLabel()}. Current version (${appMeta.version}) is up to date.`
      logEvent('info', 'No updates available')
      return
    }

    showReleasePageButton.value = true
    releasePageUrl.value = String(result.releasePageUrl || DEFAULT_RELEASES_PAGE_URL).trim() || DEFAULT_RELEASES_PAGE_URL
    configMessage.value = `New version ${result.latestVersion} available.`
    logEvent('info', `Update found: ${result.latestVersion}`)
    if (result.releaseNotes) {
      logEvent('info', `Release notes: ${result.releaseNotes}`)
    }
  } catch (err) {
    showReleasePageButton.value = false
    releasePageUrl.value = DEFAULT_RELEASES_PAGE_URL
    const message = errorMessage(err, 'Failed to check updates from GitHub Releases API.')
    configMessage.value = message
    logEvent('error', message)
  }
}

function openReleasePage() {
  openExternalUrl(releasePageUrl.value || DEFAULT_RELEASES_PAGE_URL)
}

async function openProUpgrade() {
  await refreshLicenseStatus({ silent: true })

  if (proLicense.isPro) {
    configMessage.value = `Pro is active (${proExpiryLabel.value}).`
    logEvent('info', `Checked Pro expiry (${proExpiryLabel.value})`)
    return
  }
  openRedeemDialog()
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
}

function resetTunnelTest() {
  tunnelTest.status = 'idle'
  tunnelTest.message = ''
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
    notes: form.notes.trim()
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
  if (!Number.isInteger(payload.keepAliveIntervalMs) || payload.keepAliveIntervalMs > JUMPER_LIMITS.keepAliveIntervalMax) {
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
  showJumperModal.value = true
}

function editJumper(jumper) {
  editingJumperId.value = jumper.id
  Object.assign(jumperForm, defaultJumperForm(), jumper)
  showJumperBasic.value = true
  showJumperAdvanced.value = false
  resetJumperValidation()
  resetJumperTest()
  showJumperModal.value = true
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
    notes: jumper.notes || ''
  })
}

function copyJumper(jumper) {
  editingJumperId.value = null
  fillJumperFormFromJumper(jumper, `copy-${jumper.name}`)
  showJumperBasic.value = true
  showJumperAdvanced.value = false
  resetJumperValidation()
  resetJumperTest()
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

async function testJumperConnection() {
  resetJumperValidation()
  const payload = buildJumperPayload(jumperForm)
  const error = validateJumperPayload(payload)
  if (error) {
    jumperTest.status = 'error'
    jumperTest.message = error
    return
  }

  resetJumperTest()
  jumperTest.status = 'testing'
  jumperTest.message = t('app.modals.jumper.testing')
  try {
    await TestJumperConnectionAPI(payload)
    jumperTest.status = 'success'
    jumperTest.message = 'Connection test passed.'
    logEvent('info', `Connection test passed for jumper ${payload.name}`)
  } catch (err) {
    jumperTest.status = 'error'
    jumperTest.message = errorMessage(err)
    logEvent('error', `Connection test failed for jumper ${payload.name}: ${jumperTest.message}`)
  }
}

async function testTunnelConnection() {
  resetInlineJumperValidation()
  tunnelValidationError.value = ''

  let inlinePayload = null
  let selectedJumperIds = normalizeJumperIdList(tunnelForm.jumperIds)

  if (tunnelForm.appendNewJumper) {
    inlinePayload = buildJumperPayload(inlineJumperForm)
    const inlineError = validateJumperPayload(inlinePayload)
    if (inlineError) {
      tunnelTest.status = 'error'
      tunnelTest.message = `[Jumper] ${inlineError}`
      return
    }
  }

  if (!selectedJumperIds.length && !inlinePayload) {
    tunnelTest.status = 'error'
    tunnelTest.message = 'Please select at least one jumper.'
    return
  }

  const payload = {
    name: tunnelForm.name.trim(),
    mode: tunnelForm.mode,
    jumperIds: selectedJumperIds,
    localHost: tunnelForm.localHost.trim(),
    localPort: Number(tunnelForm.localPort),
    remoteHost: tunnelForm.remoteHost.trim(),
    remotePort: Number(tunnelForm.remotePort),
    autoStart: !!tunnelForm.autoStart,
    status: 'stopped',
    description: tunnelForm.description.trim()
  }

  if (!payload.name || !payload.localHost || !payload.localPort) {
    tunnelTest.status = 'error'
    tunnelTest.message = 'Please fill in required fields.'
    return
  }
  if (payload.mode !== 'dynamic' && (!payload.remoteHost || !payload.remotePort)) {
    tunnelTest.status = 'error'
    tunnelTest.message = 'Please fill in required remote host/port.'
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
    logEvent('info', `Connection test passed for tunnel ${payload.name}; latency=${latencyText}`)
  } catch (err) {
    tunnelTest.status = 'error'
    tunnelTest.message = errorMessage(err)
    logEvent('error', `Connection test failed for tunnel ${payload.name}: ${tunnelTest.message}`)
  }
}

function openNewTunnel() {
  editingTunnelId.value = null
  Object.assign(tunnelForm, defaultTunnelForm())
  Object.assign(inlineJumperForm, defaultInlineJumperForm())
  resetInlineJumperValidation()
  resetTunnelTest()
  tunnelValidationError.value = ''
  tunnelForm.jumperIds = jumpers.value.length ? [jumpers.value[0].id] : []
  tunnelForm.nextJumperId = getNextTunnelJumperCandidate(tunnelForm.jumperIds)
  tunnelForm.appendNewJumper = jumpers.value.length === 0
  showTunnelModal.value = true
}

function openImportTunnel() {
  importTunnelError.value = ''
  showImportTunnelModal.value = true
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
            String(item.host || '').trim().toLowerCase() === String(payload.host || '').trim().toLowerCase() &&
            String(item.user || '').trim() === String(payload.user || '').trim() &&
            Number(item.port) === Number(payload.port)
          )
        })

        if (existingJumper) {
          jumperIds = [existingJumper.id]
        } else {
          const cacheKey = JSON.stringify({
            host: String(payload.host || '').trim().toLowerCase(),
            user: String(payload.user || '').trim(),
            port: Number(payload.port),
            authType: payload.authType,
            keyPath: String(payload.keyPath || '').trim(),
            agentSocketPath: String(payload.agentSocketPath || '').trim()
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
          (item) => item.host === config.host && item.user === config.user && item.port === config.port
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
            notes: `Imported from SSH command on ${new Date().toLocaleDateString()}`
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
        description: `Imported from SSH command on ${new Date().toLocaleDateString()}`
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
    
    let message = createdJumperCount > 0
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

function fillTunnelFormFromTunnel(tunnel, nameOverride = null) {
  Object.assign(tunnelForm, {
    name: nameOverride ?? tunnel.name,
    mode: tunnel.mode,
    jumperIds: normalizeJumperIdList(tunnel.jumperIds),
    nextJumperId: getNextTunnelJumperCandidate(tunnel.jumperIds),
    appendNewJumper: false,
    localHost: tunnel.localHost,
    localPort: tunnel.localPort,
    remoteHost: tunnel.remoteHost,
    remotePort: tunnel.remotePort,
    autoStart: tunnel.autoStart,
    description: tunnel.description
  })
}

function editTunnel(tunnel) {
  editingTunnelId.value = tunnel.id
  fillTunnelFormFromTunnel(tunnel)
  Object.assign(inlineJumperForm, defaultInlineJumperForm())
  resetInlineJumperValidation()
  resetTunnelTest()
  tunnelValidationError.value = ''
  showTunnelModal.value = true
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
  const ids = normalizeJumperIdList(tunnelForm.jumperIds).filter((item) => item !== id)
  tunnelForm.jumperIds = [id, ...ids]
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
  fillTunnelFormFromTunnel(tunnel, `copy-${tunnel.name}`)
  Object.assign(inlineJumperForm, defaultInlineJumperForm())
  resetInlineJumperValidation()
  resetTunnelTest()
  tunnelValidationError.value = ''
  showTunnelModal.value = true
}

async function saveTunnel() {
  let selectedJumperIds = normalizeJumperIdList(tunnelForm.jumperIds)
  resetInlineJumperValidation()
  tunnelValidationError.value = ''

  try {
    if (tunnelForm.appendNewJumper) {
      const inlinePayload = buildJumperPayload(inlineJumperForm)
      const inlineError = validateJumperPayload(inlinePayload)
      if (inlineError) {
        inlineJumperValidationError.value = inlineError
        return
      }

      const createdJumper = await CreateJumper(inlinePayload)
      selectedJumperIds = [...selectedJumperIds, createdJumper.id]
      logEvent('info', `Jumper ${createdJumper.name} created from New Tunnel`)
    }

    const editingStatus = editingTunnelId.value
      ? tunnels.value.find((item) => item.id === editingTunnelId.value)?.status || 'stopped'
      : 'stopped'
    const payload = {
      name: tunnelForm.name.trim(),
      mode: tunnelForm.mode,
      jumperIds: selectedJumperIds,
      localHost: tunnelForm.localHost.trim(),
      localPort: Number(tunnelForm.localPort),
      remoteHost: tunnelForm.remoteHost.trim(),
      remotePort: Number(tunnelForm.remotePort),
      autoStart: tunnelForm.autoStart,
      status: editingStatus === 'busy' ? 'stopped' : editingStatus,
      description: tunnelForm.description.trim()
    }

    if (!payload.name || !payload.jumperIds.length || !payload.localHost || !payload.localPort) return
    if (nameUnits(payload.name) > TUNNEL_LIMITS.name) {
      tunnelValidationError.value = 'Tunnel name must be <= 20 chars or <= 10 Chinese chars.'
      return
    }
    if (payload.mode !== 'dynamic' && (!payload.remoteHost || !payload.remotePort)) return

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
  if (!tunnel || tunnel.status === 'busy') {
    return
  }

  const shouldStart = tunnel.status === 'stopped' || tunnel.status === 'error'
  if (shouldStart && !proLicense.isPro) {
    const runningCount = tunnels.value.filter((item) => item.status === 'running').length
    const pendingStartCount = pendingToggleTunnelIds.size
    if (runningCount + pendingStartCount >= FREE_PLAN_RUNNING_LIMIT) {
      const message = locale.value === 'zh-CN'
        ? `免费版最多同时运行 ${FREE_PLAN_RUNNING_LIMIT} 个 Tunnel，请先停止一个再启动。`
        : `Free plan supports up to ${FREE_PLAN_RUNNING_LIMIT} running tunnels. Stop one before starting another.`
      openActionDialog({
        mode: 'confirm',
        message,
        confirmButtonClass: 'btn-primary',
        confirmLabel: 'Upgrade Now',
        onConfirm: async () => {
          await openProUpgrade()
        }
      })
      configMessage.value = message
      logEvent('warn', message)
      return
    }
  }

  const previousStatus = tunnel.status
  const previousLastError = tunnel.lastError || ''
  const shouldShowBusy = previousStatus === 'stopped' || previousStatus === 'error'
  if (shouldShowBusy) {
    pendingToggleTunnelIds.add(tunnel.id)
    patchTunnelLocal(tunnel.id, { status: 'busy', lastError: '', latencyMs: 0 })
  }

  // 记录启动开始时间
  const startTime = Date.now()

  try {
    const updated = await ToggleTunnel(tunnel.id)
    patchTunnelLocal(updated.id, updated)

    if (updated.status === 'error') {
      const reason = updated.lastError ? `: ${updated.lastError}` : ''
      logEvent('error', `Tunnel ${updated.name} failed${reason}`)
      return
    }

    if (previousStatus === 'error') {
      logEvent('info', `Tunnel ${tunnel.name} retry triggered`)
      return
    }

    const action = updated.status === 'running' ? 'started' : 'stopped'
    if (updated.status === 'running') {
      // 计算启动耗时
      const duration = Date.now() - startTime
      const durationText = duration < 1000 ? `${duration}ms` : `${(duration / 1000).toFixed(2)}s`
      logEvent('info', `Tunnel ${updated.name} started (took ${durationText})`)
    } else {
      logEvent('warn', `Tunnel ${updated.name} ${action}`)
    }
  } catch (err) {
    if (shouldShowBusy) {
      patchTunnelLocal(tunnel.id, { status: previousStatus, lastError: previousLastError })
    }
    logEvent('error', errorMessage(err, `Failed to toggle tunnel ${tunnel.name}`))
  } finally {
    pendingToggleTunnelIds.delete(tunnel.id)
  }
}

function openActionDialog({ mode = 'alert', message, confirmButtonClass = 'btn-primary', confirmLabel = '', onConfirm = null }) {
  actionDialog.mode = mode
  actionDialog.message = message
  actionDialog.confirmButtonClass = confirmButtonClass
  actionDialog.confirmLabel = confirmLabel
  actionDialog.onConfirm = onConfirm
  actionDialog.visible = true
}

function closeActionDialog() {
  actionDialog.visible = false
  actionDialog.confirmLabel = ''
  actionDialog.onConfirm = null
}

async function confirmActionDialog() {
  const handler = actionDialog.onConfirm
  closeActionDialog()
  if (typeof handler === 'function') {
    await handler()
  }
}

function openRedeemDialog() {
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
  const code = String(redeemDialog.code || '').trim()
  if (!code) {
    redeemDialog.error = locale.value === 'zh-CN' ? '请输入注册码。' : 'Please enter license code.'
    return
  }

  redeemDialog.error = ''
  redeemDialog.submitting = true
  try {
    const id = await ensureMachineId()
    const redeemResult = await redeemLicenseCode({ code, machineId: id })
    applyLicenseState({
      active: redeemResult?.active,
      expire_time: redeemResult?.expire_time,
      is_lifetime: redeemResult?.added_days >= LIFETIME_DURATION_DAYS
    })
    configMessage.value = `${redeemResult?.message || 'License redeemed.'} (${proExpiryLabel.value})`
    logEvent('info', `License redeemed successfully (${proExpiryLabel.value})`)
    closeRedeemDialog(true)
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
    }
  })
}

function deleteJumper(jumper) {
  const inUseBy = tunnels.value.filter((item) => normalizeJumperIdList(item.jumperIds).includes(jumper.id))
  if (inUseBy.length > 0) {
    openActionDialog({
      mode: 'alert',
      message: t('app.confirmations.deleteJumperBlocked', { name: jumper.name, count: inUseBy.length }),
      confirmButtonClass: 'btn-primary'
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
    }
  })
}

onMounted(async () => {
  await loadStateFromBackend()
  await refreshLicenseStatus({ silent: true })
  stateSyncTimer = window.setInterval(syncStateSilently, STATE_SYNC_INTERVAL_MS)
})

onBeforeUnmount(() => {
  if (stateSyncTimer !== null) {
    window.clearInterval(stateSyncTimer)
    stateSyncTimer = null
  }
})

watch(
  () => jumperForm.authType,
  (newType) => {
    if (!authShowsPassword(newType)) jumperForm.password = ''
    if (!authNeedsKeyFile(newType)) jumperForm.keyPath = ''
    resetJumperTest()
  }
)

watch(
  () => inlineJumperForm.authType,
  (newType) => {
    if (!authShowsPassword(newType)) inlineJumperForm.password = ''
    if (!authNeedsKeyFile(newType)) inlineJumperForm.keyPath = ''
  }
)
</script>


<template>
  <div class="app-shell">
    <AppSidebar
      :pages="pages"
      :active-page="activePage"
      :app-version="appMeta.version"
      :is-pro="isPro"
      :pro-expiry-label="proExpiryLabel"
      @switch-page="switchPage"
      @upgrade="openProUpgrade"
    />

    <section class="content-shell">
      <AppTopHeader
        :current-page="currentPage"
        :active-page="activePage"
        @new-jumper="openNewJumper"
        @new-tunnel="openNewTunnel"
        @import-tunnel="openImportTunnel"
      />

      <main class="page-body" :class="{ 'page-body-overview': activePage === 'overview' }">
        <OverviewPage
          v-if="activePage === 'overview'"
          :total-tunnels="totalTunnels"
          :running-tunnels="runningTunnels"
          :stopped-tunnels="stoppedTunnels"
          :auto-start-count="autoStartTunnels.length"
          :show-overview-active="showOverviewActive"
          :show-overview-activity="showOverviewActivity"
          :logs="logs"
          :get-tunnel-jumper-label="getTunnelJumperLabel"
          @toggle-overview-active="showOverviewActive = !showOverviewActive"
          @toggle-overview-activity="showOverviewActivity = !showOverviewActivity"
          @toggle-tunnel="toggleTunnel"
        />

        <JumpersPage
          v-if="activePage === 'jumpers'"
          :jumpers="filteredJumpers"
          :search-query="jumperSearchQuery"
          :get-auth-label="getAuthLabel"
          @update-search-query="jumperSearchQuery = $event"
          @copy-jumper="copyJumper"
          @edit-jumper="editJumper"
          @delete-jumper="deleteJumper"
        />

        <TunnelsPage
          v-if="activePage === 'tunnels'"
          :tunnels="filteredTunnels"
          :search-query="tunnelSearchQuery"
          :mode-options="modeOptions"
          :get-tunnel-jumper-label="getTunnelJumperLabel"
          @update-search-query="tunnelSearchQuery = $event"
          @toggle-tunnel="toggleTunnel"
          @copy-tunnel="copyTunnel"
          @edit-tunnel="editTunnel"
          @delete-tunnel="deleteTunnel"
        />

        <LogsPage
          v-if="activePage === 'logs'"
          :selected-log-level="selectedLogLevel"
          :filtered-logs="filteredLogs"
          @set-log-level="selectedLogLevel = $event"
        />

        <ConfigPage
          v-if="activePage === 'config'"
          :theme="theme"
          :app-meta="appMeta"
          :is-pro="isPro"
          :pro-expiry-label="proExpiryLabel"
          :config-message="configMessage"
          :show-release-page-button="showReleasePageButton"
          @theme-change="setThemeBySwitch"
          @check-updates="checkForUpdates"
          @open-release-page="openReleasePage"
          @upgrade="openProUpgrade"
        />
      </main>
    </section>
  </div>

  <div
    v-if="actionDialog.visible"
    class="modal fade show"
    style="display: block"
    tabindex="-1"
    aria-modal="true"
    role="dialog"
    @click.self="closeActionDialog"
  >
    <div class="modal-dialog modal-dialog-centered">
      <div class="modal-content">
        <div class="modal-header">
          <h3 class="modal-title fs-6">{{ $t('app.common.confirm') }}</h3>
          <button type="button" class="btn-close" :aria-label="$t('app.common.cancel')" @click="closeActionDialog" />
        </div>
        <div class="modal-body">
          <p class="mb-0">{{ actionDialog.message }}</p>
        </div>
        <div class="modal-footer action-dialog-footer">
          <button
            v-if="actionDialog.mode === 'confirm'"
            type="button"
            class="btn btn-outline-secondary"
            @click="closeActionDialog"
          >
            {{ $t('app.common.cancel') }}
          </button>
          <button type="button" class="btn" :class="actionDialog.confirmButtonClass" @click="confirmActionDialog">
            {{ actionDialog.confirmLabel || (actionDialog.mode === 'confirm' ? $t('app.common.delete') : $t('app.common.confirm')) }}
          </button>
        </div>
      </div>
    </div>
  </div>
  <div v-if="actionDialog.visible" class="modal-backdrop fade show" />

  <div v-if="redeemDialog.visible" class="overlay" @click.self="closeRedeemDialog">
    <div class="dialog-card compact-dialog redeem-dialog">
      <div class="dialog-head">
        <h3 class="dialog-title">{{ locale === 'zh-CN' ? '输入邀请码' : 'Enter License Code' }}</h3>
      </div>
      <form class="dialog-body" @submit.prevent="submitRedeemDialog">
        <label for="redeemCodeInput" class="form-label">{{ locale === 'zh-CN' ? '注册码' : 'License Code' }}</label>
        <input
          id="redeemCodeInput"
          v-model="redeemDialog.code"
          type="text"
          class="form-control"
          :placeholder="locale === 'zh-CN' ? '例如：VIP-XXXX-XXXX-XXXX-XXXX' : 'Example: VIP-XXXX-XXXX-XXXX-XXXX'"
          :disabled="redeemDialog.submitting"
        />
        <p v-if="redeemDialog.error" class="form-error mb-0 mt-2">{{ redeemDialog.error }}</p>
        <div class="dialog-actions mt-4">
          <div class="dialog-right-actions">
            <button
              type="button"
              class="btn btn-outline-secondary"
              :disabled="redeemDialog.submitting"
              @click="closeRedeemDialog"
            >
              {{ $t('app.common.cancel') }}
            </button>
            <button type="submit" class="btn btn-primary" :disabled="redeemDialog.submitting">
              {{ redeemDialog.submitting ? (locale === 'zh-CN' ? '提交中...' : 'Submitting...') : $t('app.sidebar.upgrade') }}
            </button>
          </div>
        </div>
      </form>
    </div>
  </div>

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
    @close="showJumperModal = false"
    @submit="saveJumper"
    @toggle-basic="showJumperBasic = !showJumperBasic"
    @toggle-advanced="showJumperAdvanced = !showJumperAdvanced"
    @key-file-change="onJumperKeyFileChange"
    @test-connection="testJumperConnection"
  />

  <TunnelModal
    :show="showTunnelModal"
    :editing-tunnel-id="editingTunnelId"
    :tunnel-form="tunnelForm"
    :mode-options="modeOptions"
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
    @close="showTunnelModal = false"
    @submit="saveTunnel"
    @set-primary-jumper="setPrimaryJumperForTunnelChain"
    @add-jumper="addJumperToTunnelChain"
    @move-jumper="moveJumperInTunnelChain"
    @trim-jumpers-to-primary="trimJumperChainToPrimary"
    @remove-jumper="removeJumperFromTunnelChain"
    @inline-key-file-change="onInlineJumperKeyFileChange"
    @test-connection="testTunnelConnection"
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
  />
</template>
