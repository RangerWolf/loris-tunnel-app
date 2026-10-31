<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'
import { getCLIMaintenance, checkCLIUpdate, importCLIUpdate, applyCLIUpdate, restorePreviousCLI, cancelCLIUpdate, openCLIDirectory } from '../../utils/cli-maintenance'

const props = defineProps({ provider: { type: String, required: true } })
const emit = defineEmits(['close', 'changed'])
const { t } = useI18n()
const status = ref({ phase: 'checking' })
const pending = ref(false)
const error = ref('')
const dialog = ref(null)
const returnFocus = typeof document !== 'undefined' ? document.activeElement : null
let timer = null, polling = false, disposed = false, statusSeq = 0
const title = computed(() => t('app.cliMaintenance.title', { name: ({ devtunnel: 'Dev Tunnel', cloudflare: 'Cloudflare', frp: 'FRP' })[props.provider] || props.provider }))
const busy = computed(() => pending.value || status.value.busy)
const replacing = computed(() => ['replacing', 'restoring'].includes(status.value.phase))
const imported = computed(() => status.value.candidateKind === 'import')
const canApply = computed(() => !busy.value && !status.value.blockedReason && !!status.value.candidateId && (imported.value || status.value.hasUpdate))
const primaryLabel = computed(() => {
  if (!status.value.installed) return t('app.cliMaintenance.install')
  if (status.value.source !== 'managed') return t('app.cliMaintenance.switchManaged')
  return t(imported.value ? 'app.cliMaintenance.replace' : 'app.cliMaintenance.upgrade')
})
const phaseText = computed(() => {
  const phase = status.value.phase
  if (phase === 'updated' && !status.value.previousVersion) return t('app.cliMaintenance.phases.installed')
  if (['checking', 'downloading', 'validating', 'replacing', 'restoring', 'updated', 'restored', 'cancelled'].includes(phase)) return t(`app.cliMaintenance.phases.${phase}`)
  if (phase === 'checked' && !imported.value) {
    if (!status.value.installed) return t('app.cliMaintenance.readyToInstall')
    if (!status.value.comparisonKnown) return t('app.cliMaintenance.cannotCompare')
    return t(status.value.hasUpdate ? 'app.cliMaintenance.updateFound' : 'app.cliMaintenance.upToDate')
  }
  return ''
})
const blockedText = computed(() => {
  const reason = status.value.blockedReason
  return reason ? t(`app.cliMaintenance.blocked.${reason}`, { count: status.value.blockedCount || 0 }) : ''
})
async function poll() {
  if (polling || disposed) return
  polling = true
  const seq = ++statusSeq
  try { const value = await getCLIMaintenance(props.provider); if (!disposed && seq === statusSeq) status.value = value }
  catch (err) { if (!disposed && seq === statusSeq) error.value = String(err?.message || err) }
  finally { polling = false }
}
async function run(action) {
  if (pending.value) return
  pending.value = true
  error.value = ''
  timer = window.setInterval(poll, 700)
  try {
    const value = await action()
    if (!disposed && value) status.value = value
  } catch (err) { if (!disposed) error.value = String(err?.message || err) }
  finally {
    window.clearInterval(timer); timer = null
    // Invalidate earlier polls before the final authoritative read.
    ++statusSeq
    if (!disposed) {
      try { status.value = await getCLIMaintenance(props.provider) } catch (err) { error.value ||= String(err?.message || err) }
      if (status.value.phase === 'cancelled') error.value = ''
      if (['updated', 'restored'].includes(status.value.phase)) emit('changed')
    }
    pending.value = false
  }
}
async function close() {
  if (replacing.value) return
  if (pending.value && !status.value.canCancel) return
  if (busy.value) {
    try {
      // The backend may have entered replacement since the last progress poll.
      if (await cancelCLIUpdate(props.provider) === false) { await poll(); return }
    }
    catch (err) { error.value = String(err?.message || err); return }
  }
  emit('close')
  nextTick(() => returnFocus?.focus())
}
async function auxiliary(action) {
  try { await action() } catch (err) { error.value = String(err?.message || err) }
}
function trap(event) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); void close() }
  if (event.key !== 'Tab') return
  const controls = [...event.currentTarget.querySelectorAll('button:not([disabled]),summary,a[href]')].filter(el => el.getClientRects().length)
  const first = controls[0], last = controls.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
onMounted(async () => {
  await nextTick(); dialog.value?.focus()
  await poll()
  if (!disposed) void run(() => checkCLIUpdate(props.provider, false))
})
onBeforeUnmount(() => { disposed = true; window.clearInterval(timer) })
</script>

<template>
  <Teleport to="body">
    <div class="devtunnel-resource-overlay" @click.self="close">
      <section ref="dialog" tabindex="-1" class="dialog-card compact-dialog devtunnel-resource-dialog cli-maintenance-dialog" role="dialog" aria-modal="true" aria-labelledby="cli-maintenance-title" :aria-busy="busy" @keydown="trap">
        <div class="dialog-head"><h3 id="cli-maintenance-title" class="dialog-title">{{ title }}</h3></div>
        <div class="dialog-body">
          <dl class="cli-maintenance-versions">
            <dt>{{ t('app.cliMaintenance.current') }}</dt><dd>{{ status.currentVersion || t('app.providers.notInstalled') }}</dd>
            <dt>{{ t(imported ? 'app.cliMaintenance.selected' : 'app.cliMaintenance.latest') }}</dt><dd>{{ (imported ? status.candidateVersion : status.latestVersion) || '—' }}</dd>
            <dt v-if="status.previousVersion">{{ t('app.cliMaintenance.previous') }}</dt>
            <dd v-if="status.previousVersion" class="cli-maintenance-previous"><span>{{ status.previousVersion }}</span><button type="button" class="btn btn-sm btn-outline-secondary" :disabled="busy || !status.canRestore || !!status.blockedReason" @click="run(() => restorePreviousCLI(provider))">{{ t('app.cliMaintenance.restore') }}</button></dd>
          </dl>
          <p v-if="phaseText" class="field-note" role="status"><i v-if="busy" class="bi bi-arrow-repeat devtunnel-spin me-1" aria-hidden="true" />{{ phaseText }}</p>
          <p v-if="status.phase === 'downloading' && status.downloaded" class="field-note" role="status">{{ (status.downloaded / 1048576).toFixed(1) }} MB<span v-if="status.total > 0"> / {{ (status.total / 1048576).toFixed(1) }} MB</span></p>
          <p v-if="blockedText" class="field-warning">{{ blockedText }}</p>
          <p v-if="status.installed && status.source !== 'managed'" class="field-note">{{ t('app.cliMaintenance.externalHint') }}</p>
          <p v-if="status.previousVersion && !status.canRestore" class="field-warning">{{ t('app.cliMaintenance.backupDamaged') }}</p>
          <p v-if="error || status.error" class="form-error" role="alert">{{ error || status.error }}</p>
          <details class="cli-maintenance-options">
            <summary>{{ t('app.cliMaintenance.more') }}</summary>
            <dl class="cli-maintenance-versions mt-2"><dt>{{ t('app.cliMaintenance.source') }}</dt><dd>{{ t(status.source === 'managed' ? 'app.cliMaintenance.managed' : 'app.cliMaintenance.external') }}</dd><dt>{{ t('app.cliMaintenance.path') }}</dt><dd class="cli-maintenance-path">{{ status.path || '—' }}</dd></dl>
            <div class="cli-maintenance-actions">
              <button type="button" class="btn btn-sm btn-outline-primary" :disabled="!status.path || replacing" @click="auxiliary(() => openCLIDirectory(provider))">{{ t('app.cliMaintenance.openDirectory') }}</button>
              <button type="button" class="btn btn-sm btn-outline-primary" :disabled="!status.docsUrl" @click="auxiliary(() => BrowserOpenURL(status.docsUrl))">{{ t('app.cliMaintenance.official') }}</button>
              <button type="button" class="btn btn-sm btn-outline-primary" :disabled="busy || !!status.blockedReason" @click="run(() => importCLIUpdate(provider))">{{ t('app.cliMaintenance.import') }}</button>
            </div>
          </details>
        </div>
        <div class="dialog-footer">
          <button type="button" class="btn btn-outline-secondary" :disabled="replacing || (pending && !status.canCancel)" @click="close">{{ t('app.cliMaintenance.close') }}</button>
          <button v-if="busy && status.canCancel" type="button" class="btn btn-outline-secondary" @click="auxiliary(() => cancelCLIUpdate(provider))">{{ t('app.providers.cancel') }}</button>
          <button v-if="!busy" type="button" class="btn btn-outline-secondary" @click="run(() => checkCLIUpdate(provider, true))">{{ t('app.cliMaintenance.recheck') }}</button>
          <button type="button" class="btn btn-primary" :disabled="!canApply" @click="run(() => applyCLIUpdate(provider, status.candidateId))">{{ primaryLabel }}</button>
        </div>
      </section>
    </div>
  </Teleport>
</template>
