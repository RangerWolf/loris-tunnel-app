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
  },
  collapsed: {
    type: Boolean,
    default: false
  }
})

defineEmits(['switch-page', 'upgrade', 'toggle-collapse'])
</script>

<template>
  <aside class="sidebar-panel" :class="{ collapsed }">
    <div class="sidebar-main">
      <button
        class="btn collapse-toggle-btn"
        type="button"
        :title="collapsed ? $t('app.sidebar.expand') : $t('app.sidebar.collapse')"
        @click="$emit('toggle-collapse')"
      >
        <i class="bi" :class="collapsed ? 'bi-chevron-right' : 'bi-chevron-left'"></i>
      </button>

      <div class="brand-logo-wrap" v-if="!collapsed">
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
          :title="collapsed ? page.title : ''"
          @click="$emit('switch-page', page.key)"
        >
          <span v-if="collapsed" class="nav-icon">
            <i v-if="page.key === 'overview'" class="bi bi-grid-1x2"></i>
            <i v-else-if="page.key === 'jumpers'" class="bi bi-router"></i>
            <i v-else-if="page.key === 'tunnels'" class="bi bi-arrow-left-right"></i>
            <i v-else-if="page.key === 'logs'" class="bi bi-journal-text"></i>
            <i v-else-if="page.key === 'config'" class="bi bi-gear"></i>
          </span>
          <span v-else>{{ page.title }}</span>
        </button>
      </div>
    </div>

    <div class="sidebar-footer" v-if="!collapsed">
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

<style scoped>
.sidebar-panel {
  position: relative;
}

.sidebar-panel.collapsed {
  width: 60px !important;
  min-width: 60px !important;
  max-width: 60px !important;
  flex: 0 0 60px !important;
}

.collapse-toggle-btn {
  position: absolute;
  top: 50%;
  right: -12px;
  transform: translateY(-50%);
  width: 24px;
  height: 24px;
  padding: 0;
  border-radius: 50%;
  background: var(--bs-body-bg);
  border: 1px solid var(--bs-border-color);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10;
  font-size: 12px;
  cursor: pointer;
}

.collapse-toggle-btn:hover {
  background: var(--bs-tertiary-bg);
}

.brand-logo-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
}

.nav-icon {
  font-size: 1.25rem;
}
</style>
