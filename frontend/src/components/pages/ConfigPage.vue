<script setup>
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  GetAutoRunEnabled,
  SetAutoRunEnabled,
  ExportConfigWithDialog,
  SelectImportFile,
  ImportConfig,
  OpenConfigDir
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
  showReleasePageButton: {
    type: Boolean,
    default: false
  },
  isCheckingUpdates: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits([
  'theme-change',
  'check-updates',
  'upgrade',
  'open-release-page',
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

watch(locale, (newLocale) => {
  localStorage.setItem('loris-tunnel.locale', newLocale)
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
            <div class="btn-group">
              <button
                type="button"
                class="btn btn-sm btn-outline-secondary"
                :disabled="configBusy !== ''"
                @click="onImportConfig"
              >
                {{ configBusy === 'import' ? '...' : t('config.importConfigBtn') }}
              </button>
              <button
                type="button"
                class="btn btn-sm btn-outline-secondary"
                :disabled="configBusy !== ''"
                @click="onExportConfig"
              >
                {{ configBusy === 'export' ? '...' : t('config.exportConfigBtn') }}
              </button>
              <button
                type="button"
                class="btn btn-sm btn-outline-secondary"
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
            <h2 class="panel-title mb-0">{{ t('config.appInfo') }}</h2>
          </div>
          <div class="info-list">
            <div class="info-row">
              <span>{{ t('config.version') }}</span>
              <strong>{{ appMeta.version }}</strong>
            </div>
            <div class="info-row">
              <span>{{ t('config.channel') }}</span>
              <strong>{{ appMeta.channel }}</strong>
            </div>
            <div class="info-row">
              <span>{{ t('config.updater') }}</span>
              <strong>{{ appMeta.updater }}</strong>
            </div>
            <div class="info-row">
              <span>{{ t('config.build') }}</span>
              <strong>{{ appMeta.build }}</strong>
            </div>
          </div>
        </div>
      </div>


      <div class="col-12">
        <div class="panel-card">
          <div class="panel-head mb-2">
            <h2 class="panel-title mb-0">{{ t('config.updateProduct') }}</h2>
          </div>
          <div class="d-flex flex-wrap gap-2">
            <button
              type="button"
              class="btn btn-outline-secondary position-relative"
              :disabled="isCheckingUpdates"
              @click="$emit('check-updates')"
            >
              <span :class="{ 'invisible': isCheckingUpdates }">{{ t('config.checkUpdates') }}</span>
              <span v-if="isCheckingUpdates" class="position-absolute top-50 start-50 translate-middle text-nowrap">
                {{ t('config.checkingUpdates') }}
              </span>
            </button>
            <button
              v-if="showReleasePageButton"
              type="button"
              class="btn btn-outline-primary"
              @click="$emit('open-release-page')"
            >
              {{ t('config.openReleases') }}
            </button>
            <button v-if="!isPro" type="button" class="btn btn-primary" @click="$emit('upgrade')">{{ t('config.upgradePro') }}</button>
            <span v-else class="pro-expiry-chip" :title="t('config.proExpires', { date: proExpiryLabel })">
              {{ t('config.proExpires', { date: proExpiryLabel }) }}
            </span>
            <button type="button" class="btn btn-outline-secondary" disabled>
              {{ t('config.manageLicense') }}
            </button>
          </div>
          <p v-if="licenseCode" class="config-message mb-0 mt-2">{{ t('config.licenseCode') }}: {{ licenseCode }}</p>
          <p v-if="configMessage" class="config-message mb-0 mt-3">{{ configMessage }}</p>
        </div>
      </div>
    </div>
  </section>
</template>
