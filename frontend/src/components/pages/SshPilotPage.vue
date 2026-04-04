<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ExecuteSSHPilotCommand,
  GetMCPInstallTemplate,
  InstallMCPToApps,
  ListMCPInstallTargets
} from '../../../wailsjs/go/main/App'
const { t } = useI18n()

const props = defineProps({
  jumpers: {
    type: Array,
    required: true
  },
  state: {
    type: Object,
    required: true
  },
  busy: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update-settings', 'refresh'])

const debugOutputMaxChars = 1000

const showDebugModal = ref(false)
const debugCommandInput = ref('')
const debugResultText = ref('')
const debugRunning = ref(false)

function truncateDebugDisplay(text) {
  const s = String(text ?? '')
  if (s.length <= debugOutputMaxChars) return s
  return `${s.slice(0, debugOutputMaxChars)}\n...(truncated)`
}

function formatDebugResult(result) {
  const parts = []
  const out = String(result?.output ?? '').trimEnd()
  if (out) parts.push(out)
  const err = String(result?.error ?? '').trim()
  if (err) parts.push(err)
  return truncateDebugDisplay(parts.join('\n'))
}

function openDebugModal() {
  debugResultText.value = ''
  showDebugModal.value = true
}

function closeDebugModal() {
  if (debugRunning.value) return
  showDebugModal.value = false
}

async function runDebugCommand() {
  const cmd = String(debugCommandInput.value ?? '').trim()
  if (!cmd || debugRunning.value) return
  debugRunning.value = true
  debugResultText.value = ''
  try {
    const result = await ExecuteSSHPilotCommand(cmd)
    debugResultText.value = formatDebugResult(result)
  } catch (e) {
    debugResultText.value = truncateDebugDisplay(String(e?.message || e || 'Error'))
  } finally {
    debugRunning.value = false
    emit('refresh')
  }
}

const groupedAllowedCommands = computed(() => {
  const source = Array.isArray(props.state?.allowedCommands) ? props.state.allowedCommands : []
  const grouped = {}
  for (const item of source) {
    const key = String(item?.category || 'common').trim() || 'common'
    if (!grouped[key]) grouped[key] = []
    grouped[key].push(item)
  }
  return Object.entries(grouped).map(([category, commands]) => ({ category, commands }))
})

const showCommandListModal = ref(false)
const customCommandInput = ref('')
const customCommandError = ref('')
const showMCPInstallDialog = ref(false)
const loadingMCPTargets = ref(false)
const installingMCP = ref(false)
const mcpInstallTargets = ref([])
const selectedMCPTargetIds = ref([])
const mcpInstallResults = ref([])
const customInstallJSON = ref('')
const installJSONCopied = ref(false)
const commandPattern = /^[A-Za-z0-9._:/-]+$/

const customCommands = computed(() => {
  return Array.isArray(props.state?.customCommands) ? props.state.customCommands : []
})

function emitSettingsUpdate(nextCustomCommands = customCommands.value) {
  emit('update-settings', {
    enabled: !!props.state?.enabled,
    selectedJumperId: Number(props.state?.selectedJumperId || 0),
    customCommands: nextCustomCommands
  })
}

function onToggleEnabled(event) {
  emit('update-settings', {
    enabled: !!event.target.checked,
    selectedJumperId: Number(props.state?.selectedJumperId || 0),
    customCommands: customCommands.value
  })
}

function onSelectJumper(event) {
  emit('update-settings', {
    enabled: !!props.state?.enabled,
    selectedJumperId: Number(event.target.value || 0),
    customCommands: customCommands.value
  })
}

function openCommandListModal() {
  customCommandInput.value = ''
  customCommandError.value = ''
  showCommandListModal.value = true
}

function closeCommandListModal() {
  customCommandInput.value = ''
  customCommandError.value = ''
  showCommandListModal.value = false
}

function addCustomCommand() {
  customCommandError.value = ''
  const cmd = String(customCommandInput.value || '').trim()
  if (!cmd) return
  if (/\s/.test(cmd)) {
    customCommandError.value = t('app.sshPilot.customErrorNoSpace')
    return
  }
  if (!commandPattern.test(cmd)) {
    customCommandError.value = t('app.sshPilot.customErrorInvalid')
    return
  }
  if (customCommands.value.includes(cmd)) {
    customCommandError.value = t('app.sshPilot.customErrorDuplicate')
    return
  }

  emitSettingsUpdate([...customCommands.value, cmd])
  customCommandInput.value = ''
}

function removeCustomCommand(command) {
  const cmd = String(command || '').trim()
  if (!cmd) return
  emitSettingsUpdate(customCommands.value.filter((item) => item !== cmd))
}

async function openMCPInstallDialog() {
  showMCPInstallDialog.value = true
  mcpInstallResults.value = []
  installJSONCopied.value = false

  loadingMCPTargets.value = true
  try {
    const [targets, template] = await Promise.all([
      ListMCPInstallTargets(),
      GetMCPInstallTemplate()
    ])
    mcpInstallTargets.value = Array.isArray(targets) ? targets : []
    selectedMCPTargetIds.value = mcpInstallTargets.value.filter((item) => item.available).map((item) => item.id)
    customInstallJSON.value = String(template || '')
  } finally {
    loadingMCPTargets.value = false
  }
}

function closeMCPInstallDialog() {
  if (installingMCP.value) return
  showMCPInstallDialog.value = false
}

async function installMCPToSelectedApps() {
  const targetIds = selectedMCPTargetIds.value.filter((id) => typeof id === 'string' && id)
  if (!targetIds.length) return

  installingMCP.value = true
  try {
    const result = await InstallMCPToApps({ targetIds })
    mcpInstallResults.value = Array.isArray(result?.results) ? result.results : []
  } finally {
    installingMCP.value = false
  }
}

async function copyInstallTemplate() {
  const text = String(customInstallJSON.value || '')
  if (!text) return
  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    }
    installJSONCopied.value = true
  } catch (_) {
    // ignore clipboard failure
  }
}
</script>

<template>
  <section class="page-fade panel-card ssh-pilot-page">
    <div class="panel-head mb-2">
      <h2 class="panel-title mb-0">{{ $t('app.sshPilot.title') }}</h2>
      <div class="d-flex align-items-center gap-2">
        <button type="button" class="btn btn-sm btn-outline-secondary compact-toggle-btn" @click="openCommandListModal">
          {{ $t('app.sshPilot.viewCommandList') }}
        </button>
        <button type="button" class="btn btn-sm btn-outline-secondary compact-toggle-btn" @click="openMCPInstallDialog">
          <i class="bi bi-cloud-arrow-down me-1" />{{ $t('app.sshPilot.installApps') }}
        </button>
        <button type="button" class="btn btn-sm btn-outline-secondary compact-toggle-btn" :disabled="busy" @click="emit('refresh')">
          <i class="bi bi-arrow-clockwise me-1" />{{ $t('app.sshPilot.refresh') }}
        </button>
        <button type="button" class="btn btn-sm btn-outline-secondary compact-toggle-btn" :disabled="busy" @click="openDebugModal">
          <i class="bi bi-bug me-1" />{{ $t('app.sshPilot.debug') }}
        </button>
      </div>
    </div>

    <div class="card-like compact-card mb-2">
      <div class="settings-grid">
        <label class="form-label compact-label mb-1" for="sshPilotJumperSelect">{{ $t('app.sshPilot.selectJumper') }}</label>
        <select
          id="sshPilotJumperSelect"
          class="form-select compact-select"
          :value="state.selectedJumperId || 0"
          :disabled="busy"
          @change="onSelectJumper"
        >
          <option :value="0">{{ $t('app.sshPilot.selectPlaceholder') }}</option>
          <option v-for="jumper in jumpers" :key="jumper.id" :value="jumper.id">
            {{ jumper.name }} ({{ jumper.user }}@{{ jumper.host }}:{{ jumper.port }})
          </option>
        </select>
      </div>

      <div class="d-flex align-items-center justify-content-between gap-3 mt-3">
        <div class="form-check form-switch m-0">
          <input
            id="sshPilotEnableSwitch"
            class="form-check-input"
            type="checkbox"
            :checked="!!state.enabled"
            :disabled="busy"
            @change="onToggleEnabled"
          >
          <label class="form-check-label ms-1" for="sshPilotEnableSwitch">{{ $t('app.sshPilot.enableMcp') }}</label>
        </div>
        <span class="status-badge" :class="state.connected ? 'status-running' : 'status-stopped'">
          {{ state.connected ? $t('app.sshPilot.connected') : $t('app.sshPilot.disconnected') }}
        </span>
      </div>

      <div class="mt-3 small text-muted">
        <div>{{ $t('app.sshPilot.protocol') }}: <strong>{{ state.protocol || '--' }}</strong></div>
        <div>{{ $t('app.sshPilot.currentJumper') }}: <strong>{{ state.selectedJumperName || '--' }}</strong></div>
        <div v-if="state.lastError" class="text-danger mt-1">{{ state.lastError }}</div>
      </div>
    </div>

    <div class="card-like compact-card">
      <div class="d-flex align-items-center justify-content-between mb-2">
        <h3 class="section-title mb-0">{{ $t('app.sshPilot.mcpLogs') }}</h3>
        <span class="text-muted small">{{ (state.logs || []).length }} entries</span>
      </div>
      <div class="table-responsive">
        <table class="table align-middle mb-0">
          <thead>
            <tr>
              <th>{{ $t('app.logs.table.time') }}</th>
              <th>{{ $t('app.logs.table.level') }}</th>
              <th>{{ $t('app.logs.table.message') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!(state.logs || []).length">
              <td colspan="3" class="text-muted py-3">{{ $t('app.sshPilot.noLogs') }}</td>
            </tr>
            <tr v-for="log in (state.logs || [])" :key="log.id">
              <td class="small text-muted">{{ log.time }}</td>
              <td>
                <span class="status-badge" :class="`status-${log.level || 'info'}`">{{ (log.level || 'info').toUpperCase() }}</span>
              </td>
              <td>{{ log.message }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>

  <div
    v-if="showCommandListModal"
    class="modal fade show"
    style="display: block"
    tabindex="-1"
    aria-modal="true"
    role="dialog"
  >
    <div class="modal-dialog modal-lg modal-dialog-centered">
      <div class="modal-content compact-dialog pilot-modal-content">
        <div class="modal-header">
          <h3 class="modal-title">{{ $t('app.sshPilot.commandListTitle') }}</h3>
          <button type="button" class="btn-close" :aria-label="$t('app.common.close')" @click="closeCommandListModal" />
        </div>
        <div class="modal-body">
          <div class="custom-command-box mb-3">
            <label class="form-label compact-label mb-1">{{ $t('app.sshPilot.customCommandsTitle') }}</label>
            <div class="d-flex gap-2 align-items-center">
              <input
                v-model.trim="customCommandInput"
                class="form-control form-control-sm flex-grow-1 min-w-0"
                :placeholder="$t('app.sshPilot.customCommandPlaceholder')"
                :disabled="busy"
                autocapitalize="off"
                autocorrect="off"
                spellcheck="false"
                @keydown.enter.prevent="addCustomCommand"
              >
              <button
                type="button"
                class="btn btn-sm btn-primary pilot-inline-primary-btn flex-shrink-0 text-nowrap"
                :disabled="busy || !customCommandInput"
                @click="addCustomCommand"
              >
                {{ $t('app.sshPilot.addCommand') }}
              </button>
            </div>
            <div class="small text-muted mt-1">{{ $t('app.sshPilot.customCommandHint') }}</div>
            <div v-if="customCommandError" class="small text-danger mt-1">{{ customCommandError }}</div>
            <div class="command-chip-list mt-2">
              <span v-if="!customCommands.length" class="small text-muted">--</span>
              <span v-for="cmd in customCommands" :key="`custom-${cmd}`" class="script-command script-command-custom">
                <code>{{ cmd }}</code>
                <button
                  type="button"
                  class="btn btn-link btn-sm script-command-remove"
                  :aria-label="$t('app.sshPilot.removeCommand')"
                  :disabled="busy"
                  @click="removeCustomCommand(cmd)"
                >×</button>
              </span>
            </div>
          </div>
          <div class="table-responsive script-table-wrap">
            <table class="table align-middle mb-0">
              <thead>
                <tr>
                  <th>{{ $t('app.sshPilot.category') }}</th>
                  <th>{{ $t('app.sshPilot.commandText') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!groupedAllowedCommands.length">
                  <td colspan="2" class="text-muted py-2">--</td>
                </tr>
                <tr v-for="group in groupedAllowedCommands" :key="group.category">
                  <td>
                    <span class="status-badge status-busy compact-chip">{{ group.category }}</span>
                  </td>
                  <td class="compact-command-cell">
                    <div class="command-chip-list">
                      <code v-for="item in group.commands" :key="item.id" class="script-command">{{ item.command }}</code>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <div class="modal-footer dialog-actions">
          <div class="dialog-right-actions">
            <button type="button" class="btn btn-outline-secondary" @click="closeCommandListModal">{{ $t('app.common.close') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
  <div v-if="showCommandListModal" class="modal-backdrop fade show" />

  <div
    v-if="showMCPInstallDialog"
    class="modal fade show"
    style="display: block"
    tabindex="-1"
    aria-modal="true"
    role="dialog"
  >
    <div class="modal-dialog modal-dialog-centered">
      <div class="modal-content compact-dialog pilot-modal-content">
        <div class="modal-header">
          <h3 class="modal-title">{{ $t('app.sshPilot.installTitle') }}</h3>
          <button type="button" class="btn-close" :aria-label="$t('app.common.close')" @click="closeMCPInstallDialog" />
        </div>
        <div class="modal-body">
          <label class="form-label compact-label mb-1">{{ $t('app.sshPilot.customInstallJSON') }}</label>
          <div class="d-flex gap-2 mb-2">
            <textarea
              class="form-control form-control-sm compact-json-textarea"
              :value="customInstallJSON"
              readonly
              spellcheck="false"
            />
            <button type="button" class="btn btn-outline-secondary btn-sm compact-copy-btn" @click="copyInstallTemplate">
              {{ installJSONCopied ? $t('app.sshPilot.copied') : $t('app.sshPilot.copyJSON') }}
            </button>
          </div>
          <p class="mb-2 small text-muted">{{ $t('app.sshPilot.installDesc') }}</p>
          <div v-if="loadingMCPTargets" class="small text-muted">{{ $t('app.sshPilot.loadingTargets') }}</div>
          <div v-else class="d-flex flex-column gap-2">
            <label v-for="target in mcpInstallTargets" :key="target.id" class="d-flex align-items-start gap-2">
              <input
                v-model="selectedMCPTargetIds"
                class="form-check-input mt-1"
                type="checkbox"
                :value="target.id"
                :disabled="installingMCP || !target.available"
              >
              <span>
                <span class="fw-semibold">{{ target.name }}</span>
                <span class="d-block small text-muted">{{ target.description }}</span>
                <code class="small">{{ target.path }}</code>
                <span v-if="!target.available" class="d-block small text-warning mt-1">{{ target.reason || $t('app.sshPilot.unavailable') }}</span>
              </span>
            </label>
          </div>
          <div v-if="mcpInstallResults.length" class="mt-3">
            <div class="small fw-semibold mb-1">{{ $t('app.sshPilot.installResult') }}</div>
            <div class="d-flex flex-column gap-1">
              <div v-for="item in mcpInstallResults" :key="`${item.targetId}-${item.installedPath}`" class="small">
                <span :class="item.success ? 'text-success' : 'text-danger'">{{ item.success ? 'OK' : 'ERR' }}</span>
                <span class="ms-1">{{ item.targetName }} · {{ item.message }}</span>
              </div>
            </div>
          </div>
        </div>
        <div class="modal-footer dialog-actions">
          <div class="dialog-right-actions">
            <button type="button" class="btn btn-outline-secondary" :disabled="installingMCP" @click="closeMCPInstallDialog">
              {{ $t('app.common.close') }}
            </button>
            <button type="button" class="btn btn-primary" :disabled="loadingMCPTargets || installingMCP" @click="installMCPToSelectedApps">
              {{ installingMCP ? $t('app.sshPilot.installing') : $t('app.sshPilot.installNow') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
  <div v-if="showMCPInstallDialog" class="modal-backdrop fade show" />

  <div
    v-if="showDebugModal"
    class="modal fade show"
    style="display: block"
    tabindex="-1"
    aria-modal="true"
    role="dialog"
  >
    <div class="modal-dialog modal-lg modal-dialog-centered">
      <div class="modal-content compact-dialog pilot-modal-content">
        <div class="modal-header">
          <h3 class="modal-title">{{ $t('app.sshPilot.debugTitle') }}</h3>
          <button type="button" class="btn-close" :aria-label="$t('app.common.close')" :disabled="debugRunning" @click="closeDebugModal" />
        </div>
        <div class="modal-body">
          <label class="form-label compact-label mb-1" for="sshPilotDebugCommand">{{ $t('app.sshPilot.debugCommandLabel') }}</label>
          <div class="d-flex gap-2 align-items-center mb-2">
            <input
              id="sshPilotDebugCommand"
              v-model="debugCommandInput"
              class="form-control form-control-sm flex-grow-1 min-w-0"
              :placeholder="$t('app.sshPilot.debugCommandPlaceholder')"
              :disabled="busy || debugRunning"
              autocapitalize="off"
              autocorrect="off"
              spellcheck="false"
              @keydown.enter.prevent="runDebugCommand"
            >
            <button
              type="button"
              class="btn btn-sm btn-primary pilot-inline-primary-btn flex-shrink-0 text-nowrap"
              :disabled="busy || debugRunning || !String(debugCommandInput || '').trim()"
              @click="runDebugCommand"
            >
              {{ debugRunning ? $t('app.sshPilot.debugRunning') : $t('app.sshPilot.debugRun') }}
            </button>
          </div>
          <p class="small text-muted mb-2">{{ $t('app.sshPilot.debugHint') }}</p>
          <label class="form-label compact-label mb-1">{{ $t('app.sshPilot.debugResultLabel') }}</label>
          <pre class="debug-result-pre mb-0">{{ debugResultText || '—' }}</pre>
        </div>
        <div class="modal-footer dialog-actions">
          <div class="dialog-right-actions">
            <button type="button" class="btn btn-outline-secondary" :disabled="debugRunning" @click="closeDebugModal">
              {{ $t('app.common.close') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
  <div v-if="showDebugModal" class="modal-backdrop fade show" />
</template>

<style scoped>
.ssh-pilot-page {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  font-size: 0.88rem;
}

.card-like {
  border: 1px solid var(--lt-border);
  border-radius: var(--lt-radius-md);
  padding: 0.8rem;
  background: var(--lt-surface-soft);
}

.compact-card {
  padding: 0.65rem 0.75rem;
}

.compact-label {
  font-size: 0.82rem;
  margin-bottom: 0.2rem !important;
}

.compact-select {
  font-size: 0.84rem;
  line-height: 1.2;
  min-height: 30px;
  height: 30px;
  padding-top: 0.2rem;
  padding-bottom: 0.2rem;
}

.compact-toggle-btn {
  height: 28px;
  min-height: 28px;
  white-space: nowrap;
  padding: 0 0.5rem;
  font-size: 0.78rem;
}

.settings-grid {
  max-width: 620px;
}

.section-title {
  font-size: 0.9rem;
  font-weight: 800;
  color: var(--lt-ink);
}

.script-table-wrap {
  max-height: 230px;
  overflow: auto;
}

.pilot-modal-content {
  font-size: 0.88rem;
}

.pilot-modal-content .modal-title {
  font-size: 0.96rem;
  font-weight: 800;
}

.compact-json-textarea {
  min-height: 102px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 0.76rem;
  resize: vertical;
}

.compact-copy-btn {
  height: 32px;
  min-width: 76px;
  white-space: nowrap;
}

.compact-command-cell {
  padding-top: 0.4rem !important;
  padding-bottom: 0.4rem !important;
}

.command-chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
}

.script-command {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.75rem;
  color: var(--lt-ink);
  border: 1px solid var(--lt-border);
  border-radius: 999px;
  background: var(--lt-surface);
  padding: 0.1rem 0.44rem;
  white-space: nowrap;
}

.compact-chip {
  font-size: 0.72rem;
  padding: 0.16rem 0.38rem;
}

.custom-command-box {
  border: 1px dashed var(--lt-border);
  border-radius: 0.5rem;
  padding: 0.6rem;
  background: var(--lt-surface-soft);
}

.script-command-custom code {
  font-size: 0.75rem;
}

.script-command-remove {
  border: 0;
  padding: 0;
  line-height: 1;
  text-decoration: none;
  color: var(--bs-danger);
}

/* Primary action beside a flex-grow input (Debug 执行 / 白名单 新增) */
.pilot-inline-primary-btn {
  min-width: 4.5rem;
  padding-left: 0.65rem;
  padding-right: 0.65rem;
}

.debug-result-pre {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 0.76rem;
  line-height: 1.35;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 280px;
  overflow: auto;
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--lt-border);
  border-radius: var(--lt-radius-md);
  background: var(--lt-surface);
  color: var(--lt-ink);
}

</style>
