<script setup>
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  GetAutoRunEnabled,
  SetAutoRunEnabled,
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
  QuitApplication
} from '../../../wailsjs/go/main/App'

const props = defineProps({
  theme: {
    type: String,
    required: true
  },
  appMeta: {
    type: Object,
    required: true
  },
  isPro: {
    type: Boolean,
    required: true
  },
  isStoreDistribution: {
    type: Boolean,
    default: false
  },
  proExpiryLabel: {
    type: String,
    required: true
  },
  licenseCode: {
    type: String,
    default: ''
  },
  configMessage: {
    type: String,
    default: ''
  },
  isCheckingUpdates: {
    type: Boolean,
    default: false
  },
  isRefreshingLicenseStatus: {
    type: Boolean,
    default: false
  },
  updateCheckDialog: {
    type: Object,
    required: true
  }
})

const emit = defineEmits([
  'theme-change',
  'check-updates',
  'upgrade',
  'open-release-page',
  'close-update-check-dialog',
  'refresh-license-status',
  'set-config-message',
  'reload-state',
  'confirm-action'
])

const { t, locale } = useI18n()
const autoRunEnabled = ref(false)
const configBusy = ref('')
const configLocationInfo = ref(null)

onMounted(async () => {
  try {
    autoRunEnabled.value = await GetAutoRunEnabled()
  } catch (_) {
    autoRunEnabled.value = false
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
  return !!(conflict?.hasConfigToml || conflict?.hasUILocale)
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
  const message = hasConflict
    ? t('config.configDirConflictPrompt', { dir })
    : messageNoConflict

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
    onConfirm: async () => run(true)
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
    apply: (overwrite) => apply(overwrite, dir)
  })
}

async function onChooseConfigDataDir() {
  try {
    await runConfigDirTargetFlow({
      resolveDir: () => SelectConfigDirectory(),
      busyKey: 'relocate',
      messageNoConflict: (dir) => t('config.configDirRelocateConfirm', { dir }),
      apply: (overwrite, dir) => SetConfigDirectory(dir, overwrite)
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
    apply: (overwrite) => ResetConfigDirectoryToDefault(overwrite)
  })
}

async function onAutoRunChange(checked) {
  if (checked && !props.isPro) {
    emit('set-config-message', t('config.autoRunProRequired'))
    emit('upgrade')
    return
  }
  try {
    await SetAutoRunEnabled(!!checked)
    autoRunEnabled.value = !!checked
  } catch (_) {
    autoRunEnabled.value = !checked
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
      }
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
</script>

<template>
  <section class="page-fade">
    <div class="row g-3">
      <div class="col-12 col-xl-6">
        <div class="panel-card config-card">
          <div class="panel-head mb-2">
            <h2 class="panel-title mb-0">{{ t('config.general') }}</h2>
          </div>

          <div class="config-row">
            <div>
              <div class="config-name">{{ t('config.language') }}</div>
              <div class="config-desc">{{ t('config.languageDesc') }}</div>
            </div>
            <div>
              <select v-model="$i18n.locale" class="form-select form-select-sm">
                <option value="en">English</option>
                <option value="zh-CN">简体中文</option>
                <option value="zh-TW">繁體中文（台灣）</option>
                <option value="zh-HK">繁體中文（香港）</option>
                <option value="ru">Русский</option>
              </select>
            </div>
          </div>

          <div class="config-row">
            <div>
              <div class="config-name">{{ t('config.theme') }}</div>
              <div class="config-desc">{{ t('config.themeDesc') }}</div>
            </div>
            <div class="form-check form-switch m-0">
              <input
                id="themeSwitch"
                class="form-check-input"
                type="checkbox"
                :checked="theme === 'dark'"
                @change="$emit('theme-change', $event.target.checked)"
              />
            </div>
          </div>

          <div class="config-row">
            <div>
              <div class="config-name">{{ t('config.autoRun') }}</div>
              <div class="config-desc">{{ t('config.autoRunDesc') }}</div>
            </div>
            <div class="form-check form-switch m-0">
              <input
                id="autoRunSwitch"
                class="form-check-input"
                type="checkbox"
                :checked="autoRunEnabled"
                @change="onAutoRunChange($event.target.checked)"
              />
            </div>
          </div>

          <div class="config-row align-items-center">
            <div>
              <div class="config-name">{{ t('config.manageConfig') }}</div>
              <div class="config-desc">{{ t('config.manageConfigDesc') }}</div>
            </div>
            <div class="btn-group" role="group" :aria-label="t('config.manageConfig')">
              <button
                type="button"
                class="btn btn-sm btn-secondary"
                :disabled="configBusy !== ''"
                @click="onImportConfig"
              >
                {{ t('config.importConfigBtn') }}
              </button>
              <button
                type="button"
                class="btn btn-sm btn-secondary"
                :disabled="configBusy !== ''"
                @click="onExportConfig"
              >
                {{ t('config.exportConfigBtn') }}
              </button>
              <button
                type="button"
                class="btn btn-sm btn-secondary"
                @click="onOpenConfigDir"
              >
                {{ t('config.openConfigDirBtn') }}
              </button>
            </div>
          </div>

          <div class="config-row align-items-start">
            <div class="flex-grow-1 min-w-0 pe-2">
              <div class="config-name">{{ t('config.configDataDir') }}</div>
              <div class="config-desc">
                <span
                  v-if="configLocationInfo?.effectiveConfigDir"
                  class="text-break d-block"
                >{{ t('config.configDirCurrentPathPrefix') }}{{ configLocationInfo.effectiveConfigDir }}</span>
                <template v-else>{{ t('config.configDirUnavailable') }}</template>
              </div>
            </div>
            <div
              class="btn-group flex-shrink-0 align-self-start"
              role="group"
              :aria-label="t('config.configDataDir')"
            >
              <button
                type="button"
                class="btn btn-sm btn-secondary"
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

        </div>
      </div>

      <div class="col-12 col-xl-6">
        <div class="panel-card config-card">
          <div class="panel-head mb-2">
            <h2 class="panel-title mb-0">{{ t('config.advancedSettings') }}</h2>
          </div>

          <div class="config-row align-items-center">
            <div>
              <div class="config-name">{{ t('config.currentVersion') }}</div>
              <div class="config-desc">{{ appMeta.version }}</div>
            </div>
            <button
              v-if="!isStoreDistribution"
              type="button"
              class="btn btn-sm btn-secondary position-relative check-updates-btn"
              :disabled="isCheckingUpdates"
              @click="$emit('check-updates')"
            >
              <span :class="{ 'invisible': isCheckingUpdates }">{{ t('config.checkUpdates') }}</span>
              <span v-if="isCheckingUpdates" class="position-absolute top-50 start-50 translate-middle text-nowrap small">
                {{ t('config.checkingUpdates') }}
              </span>
            </button>
            <span v-else class="config-desc text-muted">{{ t('config.storeUpdatesManaged') }}</span>
          </div>

          <div class="config-row align-items-center">
            <div>
              <div class="config-name">{{ t('config.licenseStatus') }}</div>
              <div class="config-desc">
                <span v-if="isPro">{{ t('config.licenseCode') }}: {{ licenseCode }}</span>
                <span v-else class="text-muted">{{ t('config.freeVersion') }}</span>
              </div>
            </div>
            <div class="btn-group" role="group" :aria-label="t('config.licenseStatus')">
              <button
                v-if="!isPro"
                type="button"
                class="btn btn-sm btn-secondary"
                @click="$emit('upgrade')"
              >
                {{ t('config.upgradePro') }}
              </button>
              <button
                v-else
                type="button"
                class="pro-badge pro-badge-btn"
                :disabled="isRefreshingLicenseStatus"
                :title="t('config.refreshLicenseStatusHint')"
                @click="$emit('refresh-license-status')"
              >
                {{ isRefreshingLicenseStatus ? t('config.refreshingLicenseStatus') : `Pro · ${proExpiryLabel}` }}
              </button>
            </div>
          </div>
        </div>
      </div>

    </div>
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
              @click="$emit('open-release-page'); $emit('close-update-check-dialog')"
            >
              {{ t('config.openDownloadPage') }}
            </button>
            <button
              type="button"
              class="btn btn-outline-secondary"
              @click="$emit('close-update-check-dialog')"
            >
              {{ t('app.common.close') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
  <div v-if="updateCheckDialog.visible" class="modal-backdrop fade show" />

</template>
