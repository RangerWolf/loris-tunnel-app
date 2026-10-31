<script setup>
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import CardHeader from '../common/CardHeader.vue'
import SecretField from '../common/SecretField.vue'
import CopyIconButton from '../common/CopyIconButton.vue'
import UpgradePrompt from '../common/UpgradePrompt.vue'
import {
  GetAutoRunEnabled,
  GetTrafficMonitorEnabled,
  SetAutoRunEnabled,
  SetTrafficMonitorEnabled,
  ExportConfigWithDialog,
  SelectImportFile,
  ImportConfig,
  OpenConfigDir,
  SaveUILocale,
  GetConfigLocationInfo,
  GetConfigDirTargetConflict,
  SelectConfigDirectory,
  SetConfigDirectory,
  ResetConfigDirectoryToDefault,
  QuitApplication,
} from '../../../wailsjs/go/main/App'

const props = defineProps({
  theme: {
    type: String,
    required: true,
  },
  accentColor: {
    type: String,
    required: true,
  },
  appMeta: {
    type: Object,
    required: true,
  },
  isPro: {
    type: Boolean,
    required: true,
  },
  isStoreDistribution: {
    type: Boolean,
    default: false,
  },
  proExpiryLabel: {
    type: String,
    required: true,
  },
  licenseCode: {
    type: String,
    default: '',
  },
  configMessage: {
    type: String,
    default: '',
  },
  isCheckingUpdates: {
    type: Boolean,
    default: false,
  },
  isRefreshingLicenseStatus: {
    type: Boolean,
    default: false,
  },
  updateCheckDialog: {
    type: Object,
    required: true,
  },

})

const emit = defineEmits([
  'theme-change',
  'accent-color-change',
  'check-updates',
  'upgrade',
  'open-release-page',
  'close-update-check-dialog',
  'refresh-license-status',
  'set-config-message',
  'reload-state',
  'confirm-action',
  'traffic-monitor-change',
])

const { t, locale } = useI18n()
const autoRunEnabled = ref(false)
const trafficMonitorEnabled = ref(true)
const configBusy = ref('')
const configLocationInfo = ref(null)
const upgradePromptVisible = ref(false)
onMounted(async () => {
  try {
    autoRunEnabled.value = await GetAutoRunEnabled()
  } catch (_) {
    autoRunEnabled.value = false
  }
  try {
    trafficMonitorEnabled.value = await GetTrafficMonitorEnabled()
  } catch (_) {
    trafficMonitorEnabled.value = true
  }
  await loadConfigLocation()
})

async function loadConfigLocation() {
  try {
    configLocationInfo.value = await GetConfigLocationInfo()
  } catch (_) {
    configLocationInfo.value = null
  }
}

function hasConfigDirFileConflict(conflict) {
  return !!(conflict?.hasConfigToml || conflict?.hasUILocale || conflict?.hasCloudSync)
}

async function getConfigDirTargetConflictSafe(dir) {
  try {
    return { ok: true, conflict: await GetConfigDirTargetConflict(dir) }
  } catch (e) {
    return { ok: false, error: String(e) }
  }
}

/** 目标目录复制迁移：冲突检测 → 覆盖/保留确认 → 执行 → 刷新路径 → 退出（打包版自动重启） */
function emitConfirmApplyConfigDirWrite({ dir, conflict, busyKey, messageNoConflict, apply }) {
  const hasConflict = hasConfigDirFileConflict(conflict)
  const message = hasConflict ? t('config.configDirConflictPrompt', { dir }) : messageNoConflict

  const run = async (overwrite) => {
    configBusy.value = busyKey
    try {
      await apply(overwrite)
      await loadConfigLocation()
      emit('set-config-message', t('config.configDirRelocateQuitHint'))
      await QuitApplication()
    } catch (err) {
      emit('set-config-message', String(err))
    } finally {
      configBusy.value = ''
    }
  }

  emit('confirm-action', {
    mode: 'confirm',
    message,
    confirmButtonClass: 'btn-warning',
    confirmLabel: hasConflict ? t('config.configDirConflictOverwrite') : t('app.common.confirm'),
    secondaryLabel: hasConflict ? t('config.configDirConflictUseExisting') : '',
    secondaryButtonClass: 'btn-outline-primary',
    onSecondary: hasConflict ? () => run(false) : null,
    onConfirm: async () => run(true),
  })
}

/**
 * 统一的「选目录 / 解析默认目录 → GetConfigDirTargetConflict → 确认框 → apply(overwrite)」管线。
 * resolveDir 返回空字符串表示取消或已在内部 toast，静默结束。
 */
async function runConfigDirTargetFlow({ resolveDir, busyKey, messageNoConflict, apply }) {
  let dir
  try {
    dir = await resolveDir()
  } catch (e) {
    emit('set-config-message', String(e))
    return
  }
  dir = typeof dir === 'string' ? dir.trim() : ''
  if (!dir) return

  const res = await getConfigDirTargetConflictSafe(dir)
  if (!res.ok) {
    emit('set-config-message', res.error)
    return
  }

  emitConfirmApplyConfigDirWrite({
    dir,
    conflict: res.conflict,
    busyKey,
    messageNoConflict: messageNoConflict(dir),
    apply: (overwrite) => apply(overwrite, dir),
  })
}

async function onChooseConfigDataDir() {
  try {
    await runConfigDirTargetFlow({
      resolveDir: () => SelectConfigDirectory(),
      busyKey: 'relocate',
      messageNoConflict: (dir) => t('config.configDirRelocateConfirm', { dir }),
      apply: (overwrite, dir) => SetConfigDirectory(dir, overwrite),
    })
  } catch (err) {
    emit('set-config-message', String(err))
  }
}

async function onResetConfigDataDir() {
  await runConfigDirTargetFlow({
    resolveDir: async () => {
      let info = configLocationInfo.value
      try {
        if (!info?.implicitConfigDir) {
          info = await GetConfigLocationInfo()
          configLocationInfo.value = info
        }
      } catch (e) {
        emit('set-config-message', String(e))
        return ''
      }
      const implicitDir = (info.implicitConfigDir ?? '').trim()
      if (!implicitDir) {
        emit('set-config-message', t('config.configDirResetImplicitMissing'))
        return ''
      }
      return implicitDir
    },
    busyKey: 'resetdir',
    messageNoConflict: () => t('config.configDirResetConfirm'),
    apply: (overwrite) => ResetConfigDirectoryToDefault(overwrite),
  })
}

async function onAutoRunChange(checked, input) {
  if (checked && !props.isPro) {
    input.checked = false
    upgradePromptVisible.value = true
    return
  }
  try {
    await SetAutoRunEnabled(!!checked)
    autoRunEnabled.value = !!checked
  } catch (_) {
    autoRunEnabled.value = !checked
  }
}

async function onTrafficMonitorChange(checked) {
  try {
    await SetTrafficMonitorEnabled(!!checked)
    trafficMonitorEnabled.value = !!checked
    emit('traffic-monitor-change', !!checked)
  } catch (_) {
    trafficMonitorEnabled.value = !checked
  }
}

async function onExportConfig() {
  configBusy.value = 'export'
  try {
    await ExportConfigWithDialog()
    emit('set-config-message', t('config.exportSuccess'))
  } catch (err) {
    emit('set-config-message', String(err))
  } finally {
    configBusy.value = ''
  }
}

async function onImportConfig() {
  configBusy.value = 'import'
  try {
    const srcPath = await SelectImportFile()
    if (!srcPath) {
      configBusy.value = ''
      return
    }
    emit('confirm-action', {
      mode: 'confirm',
      message: t('config.importConfirm'),
      confirmButtonClass: 'btn-warning',
      confirmLabel: t('app.common.confirm'),
      onConfirm: async () => {
        try {
          await ImportConfig(srcPath)
          emit('set-config-message', t('config.importSuccess'))
          emit('reload-state')
        } catch (err) {
          emit('set-config-message', String(err))
        } finally {
          configBusy.value = ''
        }
      },
    })
  } catch (err) {
    emit('set-config-message', String(err))
    configBusy.value = ''
  }
}

async function onOpenConfigDir() {
  try {
    await OpenConfigDir()
  } catch (err) {
    emit('set-config-message', String(err))
  }
}

watch(locale, async (newLocale) => {
  localStorage.setItem('loris-tunnel.locale', newLocale)
  try {
    await SaveUILocale(newLocale)
  } catch (_) {
    /* backend may be unavailable in browser-only dev */
  }
})
function compactPath(path) {
  return path.length > 52 ? `${path.slice(0, 24)}…${path.slice(-24)}` : path
}
function openReleasePage() {
  emit('open-release-page')
  emit('close-update-check-dialog')
}
</script>

<template>
  <section class="page-fade visual-page config-page">
    <div class="config-grid">
      <div class="lt-card">
        <CardHeader :title="t('config.appearance')" icon="bi-palette" />
        <div class="lt-card-body">
          <dl class="lt-kv config-kv">
            <dt class="text-body-secondary">{{ t('config.language') }}</dt>
            <dd>
              <select v-model="$i18n.locale" class="form-select form-select-sm config-appearance-select" :aria-label="t('config.language')" aria-describedby="config-language-desc">
                <option value="en">English</option>
                <option value="zh-CN">简体中文</option>
                <option value="zh-TW">繁體中文（台灣）</option>
                <option value="zh-HK">繁體中文（香港）</option>
                <option value="ru">Русский</option>
              </select>
              <small id="config-language-desc">{{ t('config.languageDesc') }}</small>
            </dd>
            <dt class="text-body-secondary">{{ t('config.brandColor') }}</dt>
            <dd>
              <div class="config-accent-swatches" role="group" :aria-label="t('config.brandColor')" aria-describedby="config-brand-color-desc">
                <button
                  type="button"
                  class="config-accent-swatch config-accent-swatch--blue"
                  :class="{ 'is-selected': accentColor === 'blue' }"
                  :aria-label="t('config.brandBlue')"
                  :aria-pressed="accentColor === 'blue'"
                  :title="t('config.brandBlue')"
                  @click="$emit('accent-color-change', 'blue')"
                >
                  <i v-if="accentColor === 'blue'" class="bi bi-check-lg" aria-hidden="true"></i>
                </button>
                <button
                  type="button"
                  class="config-accent-swatch config-accent-swatch--green"
                  :class="{ 'is-selected': accentColor === 'green' }"
                  :aria-label="t('config.brandGreen')"
                  :aria-pressed="accentColor === 'green'"
                  :title="t('config.brandGreen')"
                  @click="$emit('accent-color-change', 'green')"
                >
                  <i v-if="accentColor === 'green'" class="bi bi-check-lg" aria-hidden="true"></i>
                </button>
              </div>
              <small id="config-brand-color-desc">{{ t('config.brandColorDesc') }}</small>
            </dd>
            <dt class="text-body-secondary">{{ t('config.theme') }}</dt>
            <dd>
              <div class="form-check form-switch m-0">
                <input
                  id="themeSwitch"
                  class="form-check-input"
                  type="checkbox"
                  :checked="theme === 'dark'"
                  :aria-label="t('config.theme')"
                  aria-describedby="config-theme-desc"
                  @change="$emit('theme-change', $event.target.checked)"
                />
              </div>
              <small id="config-theme-desc">{{ t('config.themeDesc') }}</small>
            </dd>
          </dl>
        </div>
      </div>
      <div class="lt-card">
        <CardHeader :title="t('config.system')" icon="bi-sliders" />
        <div class="lt-card-body">
          <dl class="lt-kv config-kv">
            <dt class="text-body-secondary">{{ t('config.autoRun') }} <span class="lt-pill lt-pill--primary ms-1">PRO</span></dt>
            <dd>
              <div class="form-check form-switch m-0">
                <input
                  id="autoRunSwitch"
                  class="form-check-input"
                  type="checkbox"
                  :checked="autoRunEnabled"
                  :aria-label="t('config.autoRun')"
                  aria-describedby="config-auto-run-desc"
                  @change="onAutoRunChange($event.target.checked, $event.target)"
                />
              </div>
              <small id="config-auto-run-desc">{{ t('config.autoRunDesc') }}</small>
            </dd>
            <dt class="text-body-secondary">{{ t('config.trafficMonitor') }}</dt>
            <dd>
              <div class="form-check form-switch m-0">
                <input
                  id="trafficMonitorSwitch"
                  class="form-check-input"
                  type="checkbox"
                  :checked="trafficMonitorEnabled"
                  :aria-label="t('config.trafficMonitor')"
                  aria-describedby="config-traffic-desc"
                  @change="onTrafficMonitorChange($event.target.checked)"
                />
              </div>
              <small id="config-traffic-desc">{{ t('config.trafficMonitorDesc') }}</small>
            </dd>
          </dl>
        </div>
      </div>
      <div class="lt-card">
        <CardHeader :title="t('config.data')" icon="bi-folder2" />
        <div class="lt-card-body">
          <dl class="lt-kv config-kv">
            <dt class="text-body-secondary">{{ t('config.manageConfig') }}</dt>
            <dd>
              <div class="btn-group" role="group" :aria-label="t('config.manageConfig')">
                <button
                  type="button"
                  class="btn btn-sm btn-outline-secondary"
                  :disabled="configBusy !== ''"
                  @click="onImportConfig"
                >
                  {{ t('config.importConfigBtn') }}
                </button>
                <button
                  type="button"
                  class="btn btn-sm btn-outline-secondary"
                  :disabled="configBusy !== ''"
                  @click="onExportConfig"
                >
                  {{ t('config.exportConfigBtn') }}
                </button>
                <button type="button" class="btn btn-sm btn-outline-secondary" @click="onOpenConfigDir">
                  {{ t('config.openConfigDirBtn') }}
                </button>
              </div>
              <small>{{ t('config.manageConfigDesc') }}</small>
            </dd>
            <dt class="text-body-secondary">{{ t('config.configDataDir') }}</dt>
            <dd>
              <span v-if="configLocationInfo?.effectiveConfigDir" class="config-path-row">
                <span class="lt-mono config-path" :title="configLocationInfo.effectiveConfigDir">{{ compactPath(configLocationInfo.effectiveConfigDir) }}</span>
                <CopyIconButton :value="configLocationInfo.effectiveConfigDir" @error="emit('set-config-message', $event)" />
              </span>
              <small v-else>{{ t('config.configDirUnavailable') }}</small>
              <div class="config-dir-actions">
                <div class="btn-group" role="group" :aria-label="t('config.configDataDir')">
                  <button
                    type="button"
                    class="btn btn-sm btn-outline-secondary"
                    :disabled="configBusy !== ''"
                    @click="onChooseConfigDataDir"
                  >
                    {{ t('config.chooseConfigDirBtn') }}
                  </button>
                  <button
                    v-if="configLocationInfo?.isCustomConfigDir"
                    type="button"
                    class="btn btn-sm btn-outline-secondary"
                    :disabled="configBusy !== ''"
                    @click="onResetConfigDataDir"
                  >
                    {{ t('config.resetConfigDirBtn') }}
                  </button>
                </div>
              </div>
            </dd>
          </dl>
        </div>
      </div>
      <div class="lt-card">
        <CardHeader :title="t('config.aboutLicense')" icon="bi-info-circle" />
        <div class="lt-card-body">
          <dl class="lt-kv config-kv">
            <dt class="text-body-secondary">{{ t('config.currentVersion') }}</dt>
            <dd>
              <div class="d-flex align-items-center flex-wrap gap-2">
                <span>{{ appMeta.version }}</span>
                <button
                  v-if="!isStoreDistribution"
                  type="button"
                  class="btn btn-sm btn-outline-primary position-relative check-updates-btn"
                  :disabled="isCheckingUpdates"
                  @click="$emit('check-updates')"
                >
                  <span :class="{ invisible: isCheckingUpdates }">{{ t('config.checkUpdates') }}</span>
                  <span
                    v-if="isCheckingUpdates"
                    class="position-absolute top-50 start-50 translate-middle text-nowrap small"
                  >
                    {{ t('config.checkingUpdates') }}
                  </span>
                </button>
              </div>
              <small v-if="isStoreDistribution">{{ t('config.storeUpdatesManaged') }}</small>
            </dd>
            <dt class="text-body-secondary">{{ t('config.licenseStatus') }}</dt>
            <dd>
              <div class="d-flex align-items-center flex-wrap gap-2">
                <span v-if="!isPro">{{ t('config.freeVersion') }}</span>
                <button v-if="!isPro" type="button" class="btn btn-sm btn-outline-primary" @click="$emit('upgrade')">
                  {{ t('config.upgradePro') }}
                </button>
                <button
                  v-else
                  type="button"
                  class="lt-pill lt-pill--primary"
                  :disabled="isRefreshingLicenseStatus"
                  :title="t('config.refreshLicenseStatusHint')"
                  @click="$emit('refresh-license-status')"
                >
                  {{ isRefreshingLicenseStatus ? t('config.refreshingLicenseStatus') : `Pro · ${proExpiryLabel}` }}
                </button>
              </div>
              <div v-if="isPro" class="license-secret">
                <span class="text-body-secondary">{{ t('config.licenseCode') }}</span>
                <SecretField :value="licenseCode" @error="$emit('set-config-message', $event)" />
              </div>
              <div v-if="!isPro" class="mt-2 small text-body-secondary">
                <button type="button" class="btn btn-link btn-sm p-0 license-code-entry" @click="$emit('upgrade', 'activate')">{{ t('purchase.haveCode') }}</button>
              </div>
            </dd>
          </dl>
        </div>
      </div>
    </div>
    <UpgradePrompt v-if="upgradePromptVisible" :feature="t('config.autoRun')" :description="t('config.autoRunDesc')" @close="upgradePromptVisible = false" @upgrade="upgradePromptVisible = false; $emit('upgrade')" />
  </section>

  <div
    v-if="updateCheckDialog.visible"
    class="modal fade show"
    style="display: block"
    tabindex="-1"
    aria-modal="true"
    role="dialog"
  >
    <div class="modal-dialog modal-dialog-centered">
      <div class="modal-content compact-dialog update-check-dialog-content">
        <div class="modal-header dialog-head">
          <h3 class="modal-title dialog-title">{{ t('config.updateResultTitle') }}</h3>
          <button
            type="button"
            class="btn-close"
            :aria-label="t('app.common.close')"
            @click="$emit('close-update-check-dialog')"
          />
        </div>
        <div class="modal-body dialog-body">
          <p v-if="updateCheckDialog.mode === 'upToDate'" class="mb-0 update-check-dialog-text">
            {{ t('config.noUpdatesAvailable') }}
          </p>
          <p v-else-if="updateCheckDialog.mode === 'updateAvailable'" class="mb-0 update-check-dialog-text">
            {{ t('config.latestVersionIs', { version: updateCheckDialog.latestVersion }) }}
          </p>
          <p v-else class="mb-0 update-check-dialog-text">{{ updateCheckDialog.message }}</p>
        </div>
        <div class="modal-footer dialog-actions">
          <div class="dialog-right-actions">
            <button
              v-if="updateCheckDialog.mode === 'updateAvailable'"
              type="button"
              class="btn btn-primary"
              @click="openReleasePage"
            >
              {{ t('config.openDownloadPage') }}
            </button>
            <button type="button" class="btn btn-outline-secondary" @click="$emit('close-update-check-dialog')">
              {{ t('app.common.close') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
  <div v-if="updateCheckDialog.visible" class="modal-backdrop fade show" />
</template>

<style scoped>
.config-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; }
.config-kv .btn { font-weight: 400; }
.config-appearance-select { width: min(100%, 240px); }
.config-accent-swatches { display: flex; align-items: center; gap: 8px; padding: 2px 0; }
.config-accent-swatch { display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; color: #fff; border: 1px solid rgba(0, 0, 0, .12); border-radius: var(--lt-radius-md); cursor: pointer; }
.config-accent-swatch--blue { background: #1b6be0; --swatch-ring: rgba(27, 107, 224, .22); }
.config-accent-swatch--green { background: #0f766e; --swatch-ring: rgba(15, 118, 110, .22); }
.config-accent-swatch.is-selected { box-shadow: 0 0 0 2px var(--swatch-ring); }
.config-accent-swatch:focus-visible { outline: 2px solid var(--lt-ink); outline-offset: 3px; }
.config-accent-swatch .bi { font-size: 14px; line-height: 1; }
.config-path-row { display: flex; align-items: center; gap: 6px; max-width: 100%; }
.config-path { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.config-dir-actions { margin-top: 8px; }
.license-code-entry { font-size: 0.75rem; text-decoration: none; text-align: start; white-space: normal; }
.license-code-entry:hover { text-decoration: underline; }
.license-secret { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 8px; }
</style>
