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
  SaveUILocale
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

onMounted(async () => {
  try {
    autoRunEnabled.value = await GetAutoRunEnabled()
  } catch (_) {
    autoRunEnabled.value = false
  }
})

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
