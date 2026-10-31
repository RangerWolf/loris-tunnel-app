<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ParseFRPCImport } from '../../../wailsjs/go/main/App'
import { frpState, refreshFRP } from '../../utils/frp'
import { importFRPC } from '../../utils/frpc-import'
const props = defineProps({ existingTunnels: { type: Array, default: () => [] } })
const emit = defineEmits(['imported', 'busy', 'complete'])
const { t } = useI18n()
const content = ref('')
const format = ref('')
const preview = ref(null)
const rows = ref([])
const mode = ref('new')
const selectedServerID = ref('')
const serverName = ref('')
const busy = ref(false)
const error = ref('')
const summary = ref(null)
let revision = 0
watch([content, format], () => { revision++; preview.value = null; rows.value = []; summary.value = null; error.value = '' }, { flush: 'sync' })
const matches = computed(() => frpState.servers.filter(s => preview.value?.matchedServerIds.includes(s.id)))
const matched = computed(() => matches.value.find(s => s.id === selectedServerID.value))
const selectable = computed(() => preview.value?.errors.length ? [] : rows.value.filter(row => !row.errors.length && !row.imported))
const allSelected = computed(() => selectable.value.length > 0 && selectable.value.every(row => row.selected))
function selectAll(value) {
  if (busy.value) return
  for (const row of selectable.value) row.selected = value
}
function removeRow(index) {
  if (busy.value) return
  rows.value.splice(index, 1)
}
const selected = computed(() => rows.value.filter(row => row.selected && !row.errors.length && !row.imported))
const canImport = computed(() => preview.value && !preview.value.errors.length && selected.value.length > 0 &&
  selected.value.every(row => row.displayName.trim().length > 0 && row.displayName.trim().length <= 20) &&
  (mode.value === 'existing' ? !!matched.value : !!serverName.value.trim()))
function setBusy(value) { busy.value = value; emit('busy', value) }
async function readFile(event) {
  const file = event.target.files?.[0]
  if (!file) return
  setBusy(true)
  try {
    if (file.size > 2 * 1024 * 1024) throw new Error()
    content.value = await file.text()
    format.value = file.name.split('.').pop().toLowerCase()
  } catch { error.value = t('app.frpImport.fileError') }
  finally { event.target.value = ''; setBusy(false) }
}
async function parse() {
  setBusy(true); error.value = ''; preview.value = null; rows.value = []; summary.value = null
  const current = revision
  try {
    const [result] = await Promise.all([ParseFRPCImport(content.value, format.value), refreshFRP()])
    if (revision !== current) return
    preview.value = result
    rows.value = result.proxies.map(proxy => ({ ...proxy, displayName: proxy.name.slice(0, 20), selected: !proxy.errors.length, imported: false, saveError: false }))
    selectedServerID.value = matches.value[0]?.id || ''
    mode.value = matched.value ? 'existing' : 'new'
    const base = result.server.host.split('.')[0].slice(0, 30) + '-import'
    let name = base
    for (let i = 2; frpState.servers.some(s => s.name === name); i++) name = base + '-' + i
    serverName.value = name
  } catch { error.value = t('app.frpImport.parseError') }
  finally { setBusy(false) }
}
async function save() {
  if (!canImport.value || busy.value) return
  let complete = false
  setBusy(true); error.value = ''
  try {
    summary.value = await importFRPC({ content: content.value, format: format.value, existingID: mode.value === 'existing' ? matched.value.id : '', name: serverName.value, rows: rows.value, existingTunnels: props.existingTunnels })
    await refreshFRP()
    emit('imported')
    if (!summary.value.failed && !summary.value.cleanupFailed) {
      complete = true
      return
    }
    // Refresh matching after partial imports so retry uses the server just created.
    const result = await ParseFRPCImport(content.value, format.value)
    preview.value = result
    if (!matched.value) selectedServerID.value = matches.value[0]?.id || ''
    if (matched.value) mode.value = 'existing'
  } catch { error.value = t('app.frpImport.saveError') }
  finally {
    setBusy(false)
    if (complete) emit('complete')
  }
}
defineExpose({ save, canImport })
</script>

<template>
  <div class="frpc-import-panel mb-2">
    <label class="form-label" for="frpc-content">{{ t('app.frpImport.config') }}</label>
    <textarea id="frpc-content" v-model="content" class="form-control" rows="4" :disabled="busy" autocapitalize="none" autocorrect="off" spellcheck="false" placeholder='serverAddr = "frp.example.com"' @input="format = ''" />
    <div class="d-flex justify-content-end gap-2 mt-2">
      <label class="btn btn-outline-secondary mb-0" :class="{ disabled: busy }">
        {{ t('app.frpImport.file') }}
        <input type="file" class="d-none" accept=".toml,.ini,.json,.yaml,.yml" :disabled="busy" @change="readFile" />
      </label>
      <button type="button" class="btn btn-outline-primary" :disabled="busy || !content.trim()" @click="parse">{{ t('app.frpImport.parse') }}</button>
    </div>
    <p v-if="error" class="form-error mt-2" role="alert">{{ error }}</p>
    <template v-if="preview">
      <p v-for="(message, i) in preview.errors" :key="'e'+i" class="form-error">{{ message }}</p>
      <p v-for="(message, i) in preview.warnings" :key="'w'+i" class="field-warning">{{ message }}</p>
      <div class="inline-jumper-block frpc-server-block mb-2">
        <div class="block-title">{{ t('app.frpImport.serverAction') }}</div>
      <div class="row g-2 mt-1">
        <div class="col-md-6">
          <label class="form-label" for="frpc-server-mode">{{ t('app.modals.importTunnel.jumperAction') }}</label>
          <select id="frpc-server-mode" v-model="mode" class="form-select" :disabled="busy">
            <option value="new">{{ t('app.frpImport.newServer') }}</option>
            <option v-if="matches.length" value="existing">{{ t('app.frpImport.existingServer') }}</option>
          </select>
        </div>
        <div v-if="mode === 'existing'" class="col-md-6">
          <label class="form-label" for="frpc-existing-server">{{ t('app.frp.server') }}</label>
          <select id="frpc-existing-server" v-model="selectedServerID" class="form-select" :disabled="busy"><option v-for="server in matches" :key="server.id" :value="server.id">{{ server.name }} ({{ server.host }}:{{ server.port }})</option></select>
        </div>
        <div v-if="mode === 'new'" class="col-md-6">
          <label class="form-label" for="frpc-server-name">{{ t('app.frp.name') }}</label>
          <input id="frpc-server-name" v-model="serverName" type="text" class="form-control" :disabled="busy" maxlength="64" autocapitalize="none" autocorrect="off" spellcheck="false" />
        </div>
      </div>
      <div class="row g-2 mt-0">
        <div class="col-md-8">
          <label class="form-label" for="frpc-server-host">{{ t('app.frpImport.serverAddress') }}</label>
          <input id="frpc-server-host" :value="preview.server.host" type="text" class="form-control" readonly autocapitalize="none" autocorrect="off" spellcheck="false" />
        </div>
        <div class="col-md-4">
          <label class="form-label" for="frpc-server-port">{{ t('app.frpImport.serverPort') }}</label>
          <input id="frpc-server-port" :value="preview.server.port" type="number" class="form-control" readonly />
        </div>
      </div>
      <p class="field-note mt-2 mb-0">{{ t('app.frpImport.parsedServerHint') }}</p>
      <p class="field-note mt-1 mb-0">{{ t('app.frpImport.credentialsHint') }}</p>
      </div>
      <div class="form-label mt-3">{{ t('app.modals.importTunnel.parsedTunnels') }}</div>
      <div class="table-responsive mt-2">
        <table class="table align-middle">
          <thead><tr>
            <th class="text-center select-col"><input type="checkbox" :aria-label="t('app.frpImport.select')" :checked="allSelected" :indeterminate="selected.length > 0 && !allSelected" :disabled="busy || !selectable.length" @change="selectAll($event.target.checked)" /></th>
            <th>{{ t('app.modals.importTunnel.tunnelName') }}</th><th>{{ t('app.modals.importTunnel.route') }}</th><th>{{ t('app.tunnels.table.status') }}</th>
            <th class="text-end action-col">{{ t('app.common.action') }}</th>
          </tr></thead>
          <tbody>
            <tr v-for="(row, i) in rows" :key="i">
              <td class="text-center"><input v-model="row.selected" type="checkbox" :aria-label="row.name" :disabled="busy || !!row.errors.length || row.imported || !!preview.errors.length" /></td>
              <td><input v-model="row.displayName" type="text" class="form-control form-control-sm" maxlength="20" :aria-label="t('app.modals.importTunnel.tunnelName')" :disabled="busy || row.imported" autocapitalize="none" autocorrect="off" spellcheck="false" /><div v-if="row.displayName.trim() !== row.name" class="field-note">{{ t('app.frpImport.proxyName') }}: {{ row.name }}</div></td>
              <td class="text-muted">
                <div class="tunnel-route-wrap">
                  <span class="route-line">{{ row.type.toUpperCase() }}</span>
                  <span class="route-line route-line-secondary">{{ row.localIP }}:{{ row.localPort }} → {{ row.type === 'http' ? row.domain : row.remotePort }}</span>
                </div>
              </td>
              <td><span v-if="row.imported">{{ t('app.frpImport.done') }}</span><span v-else-if="row.errors.length" class="form-error">{{ row.errors.join('; ') }}</span><span v-else-if="row.saveError" class="form-error">{{ t('app.frpImport.rowError') }} {{ row.saveError }}</span><span v-else class="status-badge running">{{ t('app.modals.importTunnel.statusSuccess') }}</span></td>
              <td class="text-end"><button type="button" class="btn btn-sm btn-outline-danger icon-btn" :title="t('app.common.delete')" :aria-label="t('app.common.delete')" :disabled="busy" @click="removeRow(i)"><i class="bi bi-trash"></i></button></td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="summary" role="status">{{ t('app.frpImport.summary', summary) }}</p>
      <p v-if="summary?.cleanupFailed" class="form-error">{{ t('app.frpImport.cleanupError') }}</p>
    </template>
  </div>
</template>

<style scoped>
textarea { font-family: monospace; font-size: 0.82rem; min-height: 72px; resize: vertical; }
.table-responsive { max-height: 300px; overflow: auto; }
td { font-size: 0.78rem; }
td:nth-child(2) { min-width: 140px; }
.select-col { width: 40px; }
.action-col { width: 64px; }
.frpc-server-block { margin-top: 0.35rem; }
</style>
