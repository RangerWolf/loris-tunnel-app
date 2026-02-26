<script setup>
import { ref, computed, reactive, watch } from 'vue'

const props = defineProps({
  show: {
    type: Boolean,
    required: true
  },
  jumpers: {
    type: Array,
    required: true
  },
  existingTunnels: {
    type: Array,
    default: () => []
  },
  modeOptions: {
    type: Array,
    required: true
  },
  authOptions: {
    type: Array,
    required: true
  },
  jumperLimits: {
    type: Object,
    required: true
  },
  importError: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['close', 'import'])

const sshCommand = ref('')
const parseError = ref('')
const parsedTunnels = ref([])
const parseWarnings = ref([])
const selectedSource = ref('ssh')
const expandedErrorIds = ref(new Set())

const hasParsedTunnels = computed(() => parsedTunnels.value.length > 0)
const selectableTunnels = computed(() => parsedTunnels.value.filter((tunnel) => tunnel.importStatus === 'success'))
const hasSelectableTunnels = computed(() => selectableTunnels.value.length > 0)
const allSelectableSelected = computed(() => hasSelectableTunnels.value && selectableTunnels.value.every((tunnel) => tunnel.selected))
const parsedJumperTarget = ref(null)
const importJumperMode = ref('new')
const selectedExistingJumperId = ref('')
const importJumperValidationError = ref('')
const importJumperForm = reactive(defaultImportJumperForm())

const sourceOptions = [
  { value: 'ssh', label: 'SSH Command' }
]

function defaultImportJumperForm() {
  return {
    name: '',
    host: '',
    port: 22,
    user: '',
    authType: 'ssh_agent',
    keyPath: '',
    agentSocketPath: '',
    password: '',
    bypassHostVerification: true,
    keepAliveIntervalMs: 5000,
    timeoutMs: 10000,
    notes: ''
  }
}

const hasParsedTarget = computed(() => !!parsedJumperTarget.value)
const canUseExistingJumper = computed(() => Array.isArray(props.jumpers) && props.jumpers.length > 0)
const parsedTargetLabel = computed(() => {
  if (!parsedJumperTarget.value) return ''
  const { user, host, port } = parsedJumperTarget.value
  return `${user}@${host}:${port}`
})
const importJumperNeedsKeyFile = computed(() => importJumperForm.authType === 'ssh_key')
const importJumperNeedsPassword = computed(() => importJumperForm.authType === 'password')
const importJumperShowsPassword = computed(() => importJumperForm.authType === 'password' || importJumperForm.authType === 'ssh_key')
const matchedExistingJumper = computed(() => {
  if (!parsedJumperTarget.value) return null
  return findMatchingJumper(parsedJumperTarget.value)
})

function toHostKey(host) {
  return String(host || '').trim().toLowerCase()
}

function getTunnelSignature(tunnelLike) {
  const mode = String(tunnelLike?.mode || 'local').trim().toLowerCase()
  const localHost = toHostKey(tunnelLike?.localHost || '127.0.0.1')
  const localPort = Number(tunnelLike?.localPort) || 0
  const remoteHost = mode === 'dynamic' ? '' : toHostKey(tunnelLike?.remoteHost)
  const remotePort = mode === 'dynamic' ? 0 : (Number(tunnelLike?.remotePort) || 0)
  return `${mode}|${localHost}|${localPort}|${remoteHost}|${remotePort}`
}

const existingTunnelSignatures = computed(() => {
  return new Set((Array.isArray(props.existingTunnels) ? props.existingTunnels : []).map((tunnel) => getTunnelSignature(tunnel)))
})

function toImportJumperName(host) {
  const label = String(host || '').trim().split('.')[0] || 'import'
  return `${label}-import`
}

function findMatchingJumper(config) {
  if (!config) return null
  const host = toHostKey(config.host)
  const user = String(config.user || '').trim()
  const port = Number(config.port) || 22
  return (Array.isArray(props.jumpers) ? props.jumpers : []).find((jumper) => {
    return toHostKey(jumper.host) === host && String(jumper.user || '').trim() === user && Number(jumper.port) === port
  }) || null
}

function fillImportJumperFormFromParsed(config) {
  const parsed = config || {}
  Object.assign(importJumperForm, {
    name: toImportJumperName(parsed.host),
    host: parsed.host || '',
    port: Number(parsed.port) || 22,
    user: parsed.user || '',
    authType: parsed.keyPath ? 'ssh_key' : 'ssh_agent',
    keyPath: parsed.keyPath || '',
    agentSocketPath: '',
    password: '',
    bypassHostVerification: true,
    keepAliveIntervalMs: Number(parsed.keepAliveIntervalMs) || 5000,
    timeoutMs: 10000,
    notes: `Imported from SSH command on ${new Date().toLocaleDateString()}`
  })
}

function resetImportJumperState() {
  parsedJumperTarget.value = null
  importJumperMode.value = 'new'
  selectedExistingJumperId.value = ''
  importJumperValidationError.value = ''
  Object.assign(importJumperForm, defaultImportJumperForm())
}

function syncImportJumperFromParsed(config) {
  importJumperValidationError.value = ''
  if (!config) {
    resetImportJumperState()
    return
  }
  parsedJumperTarget.value = { ...config }
  fillImportJumperFormFromParsed(config)

  const existing = findMatchingJumper(config)
  if (existing) {
    importJumperMode.value = 'existing'
    selectedExistingJumperId.value = existing.id
    return
  }

  if (canUseExistingJumper.value) {
    selectedExistingJumperId.value = props.jumpers[0].id
  } else {
    selectedExistingJumperId.value = ''
  }
  importJumperMode.value = 'new'
}

function onImportKeyFileChange(event) {
  const file = event.target?.files && event.target.files[0]
  if (file) importJumperForm.keyPath = file.name
}

function ensureExistingJumperSelection() {
  if (!canUseExistingJumper.value) {
    importJumperMode.value = 'new'
    selectedExistingJumperId.value = ''
    return
  }
  const id = Number(selectedExistingJumperId.value)
  const exists = props.jumpers.some((jumper) => jumper.id === id)
  if (exists) return
  if (matchedExistingJumper.value) {
    selectedExistingJumperId.value = matchedExistingJumper.value.id
    return
  }
  selectedExistingJumperId.value = props.jumpers[0].id
}

function validateImportJumperPayload(payload) {
  if (!payload.name) return 'Jumper name is required.'
  if (!payload.host) return 'Jumper host is required.'
  if (!payload.user) return 'Jumper user is required.'
  if (!Number.isInteger(payload.port) || payload.port < 1 || payload.port > 65535) {
    return 'Jumper port must be between 1 and 65535.'
  }
  if (payload.authType === 'ssh_key' && !payload.keyPath) {
    return 'SSH Key mode requires key path.'
  }
  if (payload.authType === 'password' && !payload.password) {
    return 'Password mode requires password.'
  }
  if (payload.keepAliveIntervalMs > 0 && payload.keepAliveIntervalMs < 1000) {
    return 'KeepAlive interval must be 0 or >= 1000.'
  }
  return ''
}

function buildImportJumperPayload() {
  const payload = {
    name: String(importJumperForm.name || '').trim(),
    host: String(importJumperForm.host || '').trim(),
    port: Number(importJumperForm.port),
    user: String(importJumperForm.user || '').trim(),
    authType: importJumperForm.authType,
    keyPath: String(importJumperForm.keyPath || '').trim(),
    agentSocketPath: String(importJumperForm.agentSocketPath || '').trim(),
    password: importJumperForm.password || '',
    bypassHostVerification: !!importJumperForm.bypassHostVerification,
    keepAliveIntervalMs: Number(importJumperForm.keepAliveIntervalMs),
    timeoutMs: Number(importJumperForm.timeoutMs),
    notes: String(importJumperForm.notes || '').trim()
  }
  if (payload.authType !== 'ssh_key') payload.keyPath = ''
  if (payload.authType !== 'ssh_agent') payload.agentSocketPath = ''
  if (payload.authType !== 'ssh_key' && payload.authType !== 'password') payload.password = ''
  return payload
}

function parseSshCommand(cmd) {
  const tunnels = []
  let jumperConfig = null
  const warnings = {
    invalidLocal: 0,
    invalidRemote: 0,
    invalidDynamic: false,
    invalidTarget: false,
    missingTarget: false
  }
  
  // Remove extra whitespaces
  const normalizedCmd = cmd.trim().replace(/\s+/g, ' ')
  
  // Parse -L options (local port forwarding)
  const localPortRegex = /-L\s+(\S+)/g
  let match
  const localForwards = []
  
  while ((match = localPortRegex.exec(normalizedCmd)) !== null) {
    localForwards.push(match[1])
  }
  
  // Parse -R options (remote port forwarding)
  const remotePortRegex = /-R\s+(\S+)/g
  const remoteForwards = []
  
  while ((match = remotePortRegex.exec(normalizedCmd)) !== null) {
    remoteForwards.push(match[1])
  }
  
  // Parse -D option (dynamic/SOCKS proxy)
  const dynamicRegex = /-D\s+(\S+)/
  const dynamicMatch = normalizedCmd.match(dynamicRegex)
  const dynamicForward = dynamicMatch ? dynamicMatch[1] : null
  
  // Parse -i option (identity file/key path)
  const identityRegex = /-i\s+["']?([^"'\s]+)["']?/
  const identityMatch = normalizedCmd.match(identityRegex)
  const keyPath = identityMatch ? identityMatch[1] : null
  
  // Parse SSH options
  const serverAliveIntervalRegex = /-o\s+ServerAliveInterval[=\s]+(\d+)/i
  const serverAliveIntervalMatch = normalizedCmd.match(serverAliveIntervalRegex)
  const keepAliveInterval = serverAliveIntervalMatch ? parseInt(serverAliveIntervalMatch[1]) * 1000 : 5000
  
  // Parse -p option (ssh port)
  const sshPortRegex = /(?:^|\s)-p\s+(\d+)(?=\s|$)/
  const sshPortMatch = normalizedCmd.match(sshPortRegex)
  const sshPort = sshPortMatch ? parseInt(sshPortMatch[1]) : null

  function stripWrappingQuotes(token) {
    if (!token || token.length < 2) return token
    if ((token.startsWith('"') && token.endsWith('"')) || (token.startsWith("'") && token.endsWith("'"))) {
      return token.slice(1, -1)
    }
    return token
  }

  function parseTargetToken(rawToken) {
    const token = stripWrappingQuotes(String(rawToken || '').trim())
    if (!token || token.startsWith('-') || !token.includes('@')) return null

    // Use the last @ as separator so usernames like "foo@bar" are still supported.
    const atIndex = token.lastIndexOf('@')
    if (atIndex <= 0 || atIndex >= token.length - 1) return null

    const user = token.slice(0, atIndex)
    const hostPort = token.slice(atIndex + 1)
    if (!/^[A-Za-z0-9._-]+(?:@[A-Za-z0-9._-]+)*$/.test(user)) return null

    let host = hostPort
    let port = null
    const colonIndex = hostPort.lastIndexOf(':')
    if (colonIndex > 0 && hostPort.indexOf(':') === colonIndex) {
      const maybePort = hostPort.slice(colonIndex + 1)
      if (!/^\d+$/.test(maybePort)) return null
      host = hostPort.slice(0, colonIndex)
      port = parseInt(maybePort)
    }

    if (!/^[A-Za-z0-9][A-Za-z0-9.-]*$/.test(host)) return null
    if (port !== null && (!Number.isInteger(port) || port < 1 || port > 65535)) return null
    return { user, host, port }
  }

  // Parse jumper/user@host from command tokens (prefer the last valid target-like token).
  const tokens = normalizedCmd.split(' ').filter(Boolean)
  let parsedTarget = null
  let hasAtSignToken = false
  for (let i = tokens.length - 1; i >= 0; i--) {
    const token = tokens[i]
    if (!token.includes('@')) continue
    hasAtSignToken = true
    const candidate = parseTargetToken(token)
    if (candidate) {
      parsedTarget = candidate
      break
    }
  }

  if (parsedTarget) {
    jumperConfig = {
      user: parsedTarget.user,
      host: parsedTarget.host,
      port: parsedTarget.port || sshPort || 22,
      keyPath: keyPath,
      keepAliveIntervalMs: keepAliveInterval
    }
  } else if (hasAtSignToken) {
    warnings.invalidTarget = true
  }
  
  // Parse local forwards (-L)
  for (const forward of localForwards) {
    // Format: [bind_address:]port:host:hostport or [bind_address:]port:remote_socket
    const parts = forward.split(':')
    
    if (parts.length >= 3) {
      let localHost, localPort, remoteHost, remotePort
      
      if (parts.length === 3) {
        // port:host:hostport
        localHost = '127.0.0.1'
        localPort = parseInt(parts[0])
        remoteHost = parts[1]
        remotePort = parseInt(parts[2])
      } else {
        // bind_address:port:host:hostport
        localHost = parts[0] || '127.0.0.1'
        localPort = parseInt(parts[1])
        remoteHost = parts[2]
        remotePort = parseInt(parts[3])
      }
      
      if (!isNaN(localPort) && !isNaN(remotePort)) {
        tunnels.push({
          mode: 'local',
          localHost,
          localPort,
          remoteHost,
          remotePort,
          jumperConfig
        })
      } else {
        warnings.invalidLocal++
      }
    } else {
      warnings.invalidLocal++
    }
  }
  
  // Parse remote forwards (-R)
  for (const forward of remoteForwards) {
    const parts = forward.split(':')
    
    if (parts.length >= 3) {
      let remoteHost, remotePort, localHost, localPort
      
      if (parts.length === 3) {
        remoteHost = '127.0.0.1'
        remotePort = parseInt(parts[0])
        localHost = parts[1]
        localPort = parseInt(parts[2])
      } else {
        remoteHost = parts[0] || '127.0.0.1'
        remotePort = parseInt(parts[1])
        localHost = parts[2]
        localPort = parseInt(parts[3])
      }
      
      if (!isNaN(localPort) && !isNaN(remotePort)) {
        tunnels.push({
          mode: 'remote',
          localHost,
          localPort,
          remoteHost,
          remotePort,
          jumperConfig
        })
      } else {
        warnings.invalidRemote++
      }
    } else {
      warnings.invalidRemote++
    }
  }
  
  // Parse dynamic forward (-D)
  if (dynamicForward) {
    const parts = dynamicForward.split(':')
    let localHost = '127.0.0.1'
    let localPort
    
    if (parts.length === 1) {
      localPort = parseInt(parts[0])
    } else {
      localHost = parts[0] || '127.0.0.1'
      localPort = parseInt(parts[1])
    }
    
    if (!isNaN(localPort)) {
      tunnels.push({
        mode: 'dynamic',
        localHost,
        localPort,
        remoteHost: '',
        remotePort: 0,
        jumperConfig
      })
    } else {
      warnings.invalidDynamic = true
    }
  }
  
  if (!jumperConfig) {
    warnings.missingTarget = true
  }
  
  return { tunnels, jumperConfig, warnings }
}

function handleParse() {
  parseError.value = ''
  parseWarnings.value = []
  parsedTunnels.value = []
  expandedErrorIds.value = new Set()
  resetImportJumperState()
  
  if (!sshCommand.value.trim()) {
    parseError.value = 'Please enter an SSH command'
    return
  }
  
  try {
    const result = parseSshCommand(sshCommand.value)
    syncImportJumperFromParsed(result.jumperConfig)
    
    if (result.tunnels.length === 0) {
      parseError.value = 'No valid tunnel configurations found in the SSH command'
      return
    }
    
    // Generate default names for tunnels
    const seenInBatch = new Set()
    let duplicateCount = 0
    parsedTunnels.value = result.tunnels.map((tunnel, index) => {
      const signature = getTunnelSignature(tunnel)
      const duplicateExisting = existingTunnelSignatures.value.has(signature)
      const duplicateInBatch = seenInBatch.has(signature)
      const duplicate = duplicateExisting || duplicateInBatch
      if (duplicate) duplicateCount++
      seenInBatch.add(signature)

      const errorDetails = []
      if (duplicateExisting) {
        errorDetails.push({ key: 'app.modals.importTunnel.errorDuplicateExisting' })
      }
      if (duplicateInBatch) {
        errorDetails.push({ key: 'app.modals.importTunnel.errorDuplicateBatch' })
      }
      if (!tunnel.jumperConfig) {
        errorDetails.push({ key: 'app.modals.importTunnel.errorMissingTarget' })
      }

      const importStatus = errorDetails.length > 0 ? 'error' : 'success'

      const baseName = tunnel.mode === 'dynamic' 
        ? `socks-${tunnel.localPort}`
        : `${tunnel.remoteHost}-${tunnel.remotePort}`
      return {
        ...tunnel,
        id: `temp-${index}`,
        name: baseName,
        selected: importStatus === 'success',
        importStatus,
        importErrors: errorDetails
      }
    })

    if (result.warnings.invalidLocal > 0) {
      parseWarnings.value.push({
        key: 'app.modals.importTunnel.warningInvalidLocal',
        params: { count: result.warnings.invalidLocal }
      })
    }
    if (result.warnings.invalidRemote > 0) {
      parseWarnings.value.push({
        key: 'app.modals.importTunnel.warningInvalidRemote',
        params: { count: result.warnings.invalidRemote }
      })
    }
    if (result.warnings.invalidDynamic) {
      parseWarnings.value.push({
        key: 'app.modals.importTunnel.warningInvalidDynamic'
      })
    }
    if (result.warnings.invalidTarget) {
      parseWarnings.value.push({
        key: 'app.modals.importTunnel.warningInvalidTarget'
      })
    }
    if (result.warnings.missingTarget) {
      parseWarnings.value.push({
        key: 'app.modals.importTunnel.warningMissingTarget'
      })
    }
    if (duplicateCount > 0) {
      parseWarnings.value.push({
        key: 'app.modals.importTunnel.warningDuplicateSkipped',
        params: { count: duplicateCount }
      })
    }
  } catch (err) {
    resetImportJumperState()
    parseError.value = `Failed to parse SSH command: ${err.message}`
  }
}

function removeTunnel(index) {
  const [removed] = parsedTunnels.value.splice(index, 1)
  if (removed?.id) {
    const next = new Set(expandedErrorIds.value)
    next.delete(removed.id)
    expandedErrorIds.value = next
  }
}

function getModeLabel(modeValue) {
  return props.modeOptions.find((mode) => mode.value === modeValue)?.label || modeValue
}

function getRouteTop(tunnel) {
  if (tunnel.mode === 'dynamic') return `${tunnel.localHost}:${tunnel.localPort}`
  if (tunnel.mode === 'remote') return `${tunnel.remoteHost}:${tunnel.remotePort}`
  return `${tunnel.localHost}:${tunnel.localPort}`
}

function getRouteBottom(tunnel) {
  if (tunnel.mode === 'dynamic') return 'SOCKS5'
  if (tunnel.mode === 'remote') return `${tunnel.localHost}:${tunnel.localPort}`
  return `${tunnel.remoteHost}:${tunnel.remotePort}`
}

function getImportStatusLabelKey(tunnel) {
  return tunnel.importStatus === 'error'
    ? 'app.modals.importTunnel.statusError'
    : 'app.modals.importTunnel.statusSuccess'
}

function getImportStatusBadgeClass(tunnel) {
  return tunnel.importStatus === 'error' ? 'error' : 'running'
}

function canToggleErrorDetails(tunnel) {
  return tunnel.importStatus === 'error' && Array.isArray(tunnel.importErrors) && tunnel.importErrors.length > 0
}

function isErrorExpanded(tunnelId) {
  return expandedErrorIds.value.has(tunnelId)
}

function toggleErrorDetails(tunnel) {
  if (!canToggleErrorDetails(tunnel)) return
  const next = new Set(expandedErrorIds.value)
  if (next.has(tunnel.id)) {
    next.delete(tunnel.id)
  } else {
    next.add(tunnel.id)
  }
  expandedErrorIds.value = next
}

function handleImport() {
  importJumperValidationError.value = ''
  const selectedTunnels = parsedTunnels.value.filter(t => t.selected)
  
  if (selectedTunnels.length === 0) {
    parseError.value = 'Please select at least one tunnel to import'
    return
  }
  
  // Check for duplicate names
  const names = selectedTunnels.map(t => t.name)
  const uniqueNames = new Set(names)
  if (uniqueNames.size !== names.length) {
    parseError.value = 'Duplicate tunnel names found. Please make each name unique.'
    return
  }

  let importJumper = null
  if (hasParsedTarget.value) {
    if (importJumperMode.value === 'existing') {
      ensureExistingJumperSelection()
      const jumperId = Number(selectedExistingJumperId.value)
      if (!Number.isInteger(jumperId) || jumperId <= 0) {
        importJumperValidationError.value = 'Please select one existing jumper.'
        return
      }
      importJumper = { mode: 'existing', jumperId }
    } else {
      const payload = buildImportJumperPayload()
      const validationError = validateImportJumperPayload(payload)
      if (validationError) {
        importJumperValidationError.value = validationError
        return
      }
      importJumper = { mode: 'new', payload }
    }
  }

  emit('import', selectedTunnels.map((tunnel) => ({ ...tunnel, importJumper })))
}

function resetFormState() {
  sshCommand.value = ''
  parseError.value = ''
  parseWarnings.value = []
  parsedTunnels.value = []
  selectedSource.value = 'ssh'
  expandedErrorIds.value = new Set()
  resetImportJumperState()
}

function handleClose() {
  resetFormState()
  emit('close')
}

watch(importJumperMode, (mode) => {
  if (mode === 'existing') {
    ensureExistingJumperSelection()
  }
  importJumperValidationError.value = ''
})

watch(
  () => props.jumpers,
  () => {
    if (importJumperMode.value === 'existing') {
      ensureExistingJumperSelection()
    }
  },
  { deep: true }
)

watch(
  () => props.show,
  (visible) => {
    if (!visible) {
      resetFormState()
    }
  }
)
</script>

<template>
  <div v-if="show" class="overlay" @click.self="handleClose">
    <div class="dialog-card dialog-large compact-dialog import-tunnel-dialog">
      <div class="dialog-head">
        <h3 class="dialog-title">{{ $t('app.modals.importTunnel.title') }}</h3>
      </div>
      <div class="dialog-body">
        <div class="mb-2">
          <label class="form-label">{{ $t('app.modals.importTunnel.source') }}</label>
          <select v-model="selectedSource" class="form-select">
            <option v-for="option in sourceOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </select>
        </div>

        <div v-if="selectedSource === 'ssh'" class="mb-2">
          <label class="form-label">{{ $t('app.modals.importTunnel.sshCommand') }}</label>
          <textarea
            v-model="sshCommand"
            class="form-control ssh-command-textarea"
            rows="4"
            :placeholder="$t('app.modals.importTunnel.sshCommandPlaceholder')"
          />
          <div class="d-flex justify-content-end mt-2">
            <button type="button" class="btn btn-outline-primary parse-btn" @click="handleParse">
              {{ $t('app.modals.importTunnel.parseCmd') }}
            </button>
          </div>
        </div>

        <p v-if="parseError" class="form-error import-parse-error mb-2">{{ parseError }}</p>
        <p v-if="props.importError" class="form-error import-parse-error mb-2">{{ props.importError }}</p>
        <div v-if="parseWarnings.length > 0" class="import-parse-warnings mb-2">
          <div v-for="(item, idx) in parseWarnings" :key="`parse-warning-${idx}`" class="import-parse-warning-item">
            {{ $t(item.key, item.params || {}) }}
          </div>
        </div>

        <div v-if="hasParsedTarget" class="inline-jumper-block import-jumper-block mb-2">
          <div class="block-title">{{ $t('app.modals.importTunnel.jumperSectionTitle') }}</div>
          <div class="field-note mt-1">
            {{ $t('app.modals.importTunnel.parsedTargetLabel', { target: parsedTargetLabel }) }}
          </div>
          <div class="row g-2 mt-0 import-jumper-grid">
            <div class="col-md-6">
              <label class="form-label">{{ $t('app.modals.importTunnel.jumperAction') }}</label>
              <select v-model="importJumperMode" class="form-select">
                <option :value="'existing'" :disabled="!canUseExistingJumper">{{ $t('app.modals.importTunnel.useExistingJumper') }}</option>
                <option :value="'new'">{{ $t('app.modals.importTunnel.createNewJumper') }}</option>
              </select>
            </div>
            <div v-if="importJumperMode === 'existing'" class="col-md-6">
              <label class="form-label">{{ $t('app.modals.importTunnel.existingJumper') }}</label>
              <select v-model.number="selectedExistingJumperId" class="form-select">
                <option v-for="jumper in jumpers" :key="`import-existing-jumper-${jumper.id}`" :value="jumper.id">
                  {{ jumper.name }} ({{ jumper.user }}@{{ jumper.host }}:{{ jumper.port }})
                </option>
              </select>
              <div v-if="matchedExistingJumper" class="field-note mt-1">
                {{ $t('app.modals.importTunnel.defaultMatchedJumper') }}
              </div>
            </div>
            <div v-else class="col-md-6">
              <label class="form-label">{{ $t('app.modals.jumper.name') }}</label>
              <input v-model="importJumperForm.name" class="form-control" type="text" :maxlength="jumperLimits.name" required />
            </div>
          </div>

          <div v-if="importJumperMode === 'new'" class="row g-2 mt-0 import-jumper-grid">
            <div class="import-jumper-target-row">
              <div>
              <label class="form-label">{{ $t('app.modals.jumper.user') }}</label>
              <input v-model="importJumperForm.user" class="form-control" type="text" :maxlength="jumperLimits.user" required />
              </div>
              <div>
              <label class="form-label">{{ $t('app.modals.jumper.host') }}</label>
              <input v-model="importJumperForm.host" class="form-control" type="text" :maxlength="jumperLimits.host" required />
              </div>
              <div>
              <label class="form-label">{{ $t('app.modals.jumper.port') }}</label>
              <input v-model.number="importJumperForm.port" class="form-control" type="number" min="1" max="65535" required />
              </div>
            </div>
            <div class="col-md-6">
              <label class="form-label">{{ $t('app.modals.jumper.authMethod') }}</label>
              <select v-model="importJumperForm.authType" class="form-select">
                <option v-for="option in authOptions" :key="`import-jumper-auth-${option.value}`" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
              <div v-if="importJumperForm.authType === 'ssh_agent'" class="field-note mt-1">
                {{ $t('app.modals.jumper.sshAgentNote') }}
              </div>
            </div>
            <div v-if="importJumperForm.authType === 'ssh_agent'" class="col-md-6">
              <label class="form-label">{{ $t('app.modals.jumper.agentSocketPath') }}</label>
              <input
                v-model="importJumperForm.agentSocketPath"
                class="form-control"
                type="text"
                :maxlength="jumperLimits.agentSocketPath"
                :placeholder="$t('app.modals.jumper.agentSocketPlaceholder')"
              />
              <div class="field-note">{{ $t('app.modals.jumper.agentSocketNote') }}</div>
            </div>
            <template v-if="importJumperNeedsKeyFile">
              <div class="col-md-7">
                <label class="form-label">{{ $t('app.modals.jumper.sshKeyFile') }}</label>
                <div class="input-group">
                  <input
                    v-model="importJumperForm.keyPath"
                    class="form-control"
                    type="text"
                    :maxlength="jumperLimits.keyPath"
                    :placeholder="$t('app.modals.jumper.keyPathPlaceholder')"
                    :required="importJumperNeedsKeyFile"
                  />
                  <label class="btn btn-outline-secondary mb-0">
                    {{ $t('app.modals.jumper.browse') }}
                    <input class="d-none" type="file" @change="onImportKeyFileChange" />
                  </label>
                </div>
                <div class="field-note">{{ $t('app.modals.jumper.keyFileNote') }}</div>
              </div>
              <div class="col-md-5">
                <label class="form-label">{{ $t('app.modals.jumper.password') }}</label>
                <input
                  v-model="importJumperForm.password"
                  class="form-control"
                  type="password"
                  :maxlength="jumperLimits.password"
                  :placeholder="$t('app.modals.jumper.passwordOptionalPlaceholder')"
                  :required="importJumperNeedsPassword"
                />
              </div>
            </template>
            <div v-else-if="importJumperShowsPassword" class="col-md-12">
              <label class="form-label">{{ $t('app.modals.jumper.password') }}</label>
              <input
                v-model="importJumperForm.password"
                class="form-control"
                type="password"
                :maxlength="jumperLimits.password"
                :placeholder="$t('app.modals.jumper.passwordPlaceholder')"
                :required="importJumperNeedsPassword"
              />
            </div>
            <div class="col-md-6">
              <label class="form-label">{{ $t('app.modals.jumper.keepAliveInterval') }}</label>
              <input
                v-model.number="importJumperForm.keepAliveIntervalMs"
                class="form-control"
                type="number"
                min="0"
                :max="jumperLimits.keepAliveIntervalMax"
              />
            </div>
            <div class="col-md-6">
              <label class="form-label">{{ $t('app.modals.jumper.timeout') }}</label>
              <input
                v-model.number="importJumperForm.timeoutMs"
                class="form-control"
                type="number"
                :min="jumperLimits.timeoutMin"
                :max="jumperLimits.timeoutMax"
              />
            </div>
            <div class="col-md-12">
              <label class="form-label">{{ $t('app.modals.jumper.notes') }}</label>
              <textarea v-model="importJumperForm.notes" class="form-control" rows="2" :maxlength="jumperLimits.notes" />
            </div>
            <div class="col-md-12">
              <div class="form-check form-switch">
                <input id="importBypassHostSwitch" v-model="importJumperForm.bypassHostVerification" class="form-check-input" type="checkbox" />
                <label class="form-check-label" for="importBypassHostSwitch">{{ $t('app.modals.jumper.bypassHostCheck') }}</label>
              </div>
            </div>
          </div>

          <p v-if="importJumperValidationError" class="form-error mb-0 mt-2">{{ importJumperValidationError }}</p>
        </div>

        <div v-if="hasParsedTunnels" class="parsed-tunnels-section">
          <label class="form-label">{{ $t('app.modals.importTunnel.parsedTunnels') }}</label>
          <div class="table-responsive parsed-tunnels-table">
            <table class="table align-middle mb-0 tunnels-table import-tunnels-table">
              <thead>
                <tr>
                  <th class="text-center import-select-col">
                    <input
                      type="checkbox"
                      :checked="allSelectableSelected"
                      @change="$event.target.checked ? parsedTunnels.forEach(t => { if (t.importStatus === 'success') t.selected = true }) : parsedTunnels.forEach(t => t.selected = false)"
                    />
                  </th>
                  <th class="tunnel-name-cell">{{ $t('app.modals.importTunnel.tunnelName') }}</th>
                  <th class="tunnel-mode-cell">{{ $t('app.modals.importTunnel.mode') }}</th>
                  <th class="tunnel-route-cell">{{ $t('app.modals.importTunnel.route') }}</th>
                  <th class="tunnel-status-cell">{{ $t('app.tunnels.table.status') }}</th>
                  <th class="text-end tunnels-action-cell import-action-col">{{ $t('app.common.action') }}</th>
                </tr>
              </thead>
              <tbody>
                <template v-for="(tunnel, index) in parsedTunnels" :key="tunnel.id">
                  <tr :class="{ 'import-row-error': tunnel.importStatus === 'error' }">
                    <td class="text-center">
                      <input type="checkbox" v-model="tunnel.selected" :disabled="tunnel.importStatus === 'error'" />
                    </td>
                    <td>
                      <input
                        v-model="tunnel.name"
                        class="form-control form-control-sm"
                        type="text"
                        :disabled="tunnel.importStatus === 'error'"
                      />
                    </td>
                    <td class="tunnel-mode-cell">{{ getModeLabel(tunnel.mode) }}</td>
                    <td class="text-muted tunnel-route-cell">
                      <div class="tunnel-route-wrap">
                        <span class="route-line cell-ellipsis" :title="getRouteTop(tunnel)">{{ getRouteTop(tunnel) }}</span>
                        <span class="route-line route-line-secondary cell-ellipsis" :title="getRouteBottom(tunnel)">{{ getRouteBottom(tunnel) }}</span>
                      </div>
                    </td>
                    <td class="tunnel-status-cell">
                      <div class="tunnel-status-wrap">
                        <span
                          class="status-badge"
                          :class="[getImportStatusBadgeClass(tunnel), { 'status-badge-expandable': canToggleErrorDetails(tunnel) }]"
                          :role="canToggleErrorDetails(tunnel) ? 'button' : undefined"
                          :tabindex="canToggleErrorDetails(tunnel) ? 0 : undefined"
                          :aria-label="canToggleErrorDetails(tunnel) ? $t(isErrorExpanded(tunnel.id) ? 'app.tunnels.actions.collapseError' : 'app.tunnels.actions.expandError') : undefined"
                          :aria-expanded="canToggleErrorDetails(tunnel) ? isErrorExpanded(tunnel.id) : undefined"
                          @click="toggleErrorDetails(tunnel)"
                          @keydown.enter.prevent="toggleErrorDetails(tunnel)"
                          @keydown.space.prevent="toggleErrorDetails(tunnel)"
                        >
                          <span>{{ $t(getImportStatusLabelKey(tunnel)) }}</span>
                          <i
                            v-if="canToggleErrorDetails(tunnel)"
                            class="bi status-badge-toggle-icon"
                            :class="isErrorExpanded(tunnel.id) ? 'bi-chevron-up' : 'bi-chevron-down'"
                          />
                        </span>
                      </div>
                    </td>
                    <td class="text-end tunnels-action-cell">
                      <button
                        type="button"
                        class="btn btn-sm btn-outline-danger icon-btn"
                        :title="$t('app.common.delete')"
                        @click="removeTunnel(index)"
                      >
                        <i class="bi bi-trash"></i>
                      </button>
                    </td>
                  </tr>
                  <tr v-if="isErrorExpanded(tunnel.id)" class="tunnel-error-detail-row">
                    <td colspan="6">
                      <div class="tunnel-error-detail">
                        <div class="tunnel-error-detail-content">
                          <div class="tunnel-error-detail-label">{{ $t('app.tunnels.errorReason') }}</div>
                          <div class="tunnel-error-detail-message">
                            <div v-for="(err, errIndex) in tunnel.importErrors" :key="`import-error-${tunnel.id}-${errIndex}`">
                              {{ $t(err.key, err.params || {}) }}
                            </div>
                          </div>
                        </div>
                      </div>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </div>
      </div>
      <div class="dialog-footer import-dialog-footer">
        <div class="dialog-right-actions">
          <button type="button" class="btn btn-outline-secondary" @click="handleClose">
            {{ $t('app.common.cancel') }}
          </button>
            <button
              type="button"
              class="btn btn-primary"
              :disabled="!hasSelectableTunnels || !parsedTunnels.some(t => t.selected)"
              @click="handleImport"
            >
              {{ $t('app.modals.importTunnel.importBtn') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ssh-command-textarea {
  font-family: monospace;
  font-size: 0.82rem;
  min-height: 72px;
  resize: vertical;
}

.parse-btn {
  font-size: 0.8rem;
  padding: 0.375rem 0.75rem;
  min-height: 32px;
}

.import-jumper-block {
  margin-top: 0.35rem;
}

.import-jumper-grid {
  --bs-gutter-y: 0.5rem;
}

.import-jumper-target-row {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.5rem;
  width: 100%;
}

@media (min-width: 768px) {
  .import-jumper-target-row {
    grid-template-columns: 4fr 4fr 2fr;
  }
}

.parsed-tunnels-section {
  margin-top: 0.75rem;
}

.parsed-tunnels-table {
  max-height: 300px;
  overflow: auto;
}

.import-tunnels-table .import-select-col {
  width: 40px;
  max-width: 40px;
}

.import-tunnels-table .import-action-col {
  width: 64px;
  max-width: 64px;
}

.import-tunnels-table .tunnel-name-cell {
  width: 138px;
  max-width: 138px;
}

.import-tunnels-table .tunnel-mode-cell {
  width: 136px;
  max-width: 136px;
  white-space: nowrap;
}

.import-tunnels-table .tunnel-route-cell {
  width: auto;
  max-width: none;
}

.import-tunnels-table .tunnel-status-cell {
  width: 120px;
  max-width: 120px;
}

.import-tunnels-table .form-control.form-control-sm {
  min-height: 30px;
}

.import-parse-error {
  margin-top: 0.2rem;
  padding: 0.48rem 0.62rem;
  border: 1px solid var(--lt-danger-border);
  border-radius: 7px;
  background: var(--lt-danger-bg);
  font-size: 0.76rem;
  line-height: 1.35;
}

.import-parse-warnings {
  border: 1px solid var(--lt-warning-border);
  border-radius: 7px;
  background: var(--lt-warning-bg);
  color: var(--lt-warning-ink);
  padding: 0.45rem 0.62rem;
  font-size: 0.75rem;
}

.import-parse-warning-item + .import-parse-warning-item {
  margin-top: 0.18rem;
}

.import-row-error td {
  background: rgba(239, 200, 141, 0.16);
}

.import-dialog-footer {
  padding: 0.72rem 0.92rem;
}

.import-dialog-footer .btn {
  font-size: 0.8rem;
  padding: 0.375rem 0.75rem;
  min-height: 32px;
}

@media (max-width: 768px) {
  .parse-btn {
    width: 100%;
  }
}
</style>
