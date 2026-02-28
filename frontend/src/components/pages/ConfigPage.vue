<script setup>
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'

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
  configMessage: {
    type: String,
    default: ''
  },
  showReleasePageButton: {
    type: Boolean,
    default: false
  }
})

defineEmits(['theme-change', 'check-updates', 'upgrade', 'open-release-page'])

const { t, locale } = useI18n()

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
            <h2 class="panel-title mb-0">{{ t('config.appearance') }}</h2>
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
            <button type="button" class="btn btn-outline-secondary" @click="$emit('check-updates')">
              {{ t('config.checkUpdates') }}
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
          <p v-if="configMessage" class="config-message mb-0 mt-3">{{ configMessage }}</p>
        </div>
      </div>
    </div>
  </section>
</template>
