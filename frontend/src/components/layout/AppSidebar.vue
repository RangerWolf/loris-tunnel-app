<script setup>
defineProps({
  pages: {
    type: Array,
    required: true
  },
  activePage: {
    type: String,
    required: true
  },
  appVersion: {
    type: String,
    required: true
  },
  isPro: {
    type: Boolean,
    required: true
  },
  proExpiryLabel: {
    type: String,
    required: true
  }
})

defineEmits(['switch-page', 'upgrade'])
</script>

<template>
  <aside class="sidebar-panel">
    <div class="sidebar-main">
      <div class="brand-logo-wrap">
        <div class="brand-dot" />
        <div>
          <div class="brand-title">{{ $t('app.title') }}</div>
          <div class="brand-subtitle">{{ $t('app.sidebar.subtitle') }}</div>
        </div>
      </div>

      <div class="nav-list mt-4">
        <button
          v-for="page in pages"
          :key="page.key"
          type="button"
          class="btn nav-item-btn"
          :class="{ active: activePage === page.key }"
          @click="$emit('switch-page', page.key)"
        >
          {{ page.title }}
        </button>
      </div>
    </div>

    <div class="sidebar-footer">
      <span class="sidebar-version">v{{ appVersion }}</span>
      <button
        v-if="!isPro"
        type="button"
        class="btn btn-primary btn-sm sidebar-upgrade-btn"
        :title="$t('app.sidebar.upgrade')"
        :aria-label="$t('app.sidebar.upgrade')"
        @click="$emit('upgrade')"
      >
        {{ $t('app.sidebar.upgrade') }}
      </button>
      <span v-else class="sidebar-pro-expiry" :title="$t('config.proExpires', { date: proExpiryLabel })">
        Pro · {{ proExpiryLabel }}
      </span>
    </div>
  </aside>
</template>
