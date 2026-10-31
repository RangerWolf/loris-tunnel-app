<script setup>
import LogEntriesTable from '../common/LogEntriesTable.vue'

defineProps({
  selectedLogLevel: {
    type: String,
    required: true
  },
  filteredLogs: {
    type: Array,
    required: true
  }
})

defineEmits(['set-log-level'])
</script>

<template>
  <section class="page-fade panel-card">
    <div class="panel-head mb-3">
      <h2 class="panel-title mb-0">{{ $t('app.logs.title') }}</h2>
      <div class="btn-group btn-group-sm lt-log-levels" role="group" :aria-label="$t('app.logs.title')">
        <button
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: selectedLogLevel === 'all' }"
          :aria-pressed="selectedLogLevel === 'all'"
          @click="$emit('set-log-level', 'all')"
        >
          {{ $t('app.logs.levels.all') }}
        </button>
        <button
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: selectedLogLevel === 'info' }"
          :aria-pressed="selectedLogLevel === 'info'"
          @click="$emit('set-log-level', 'info')"
        >
          {{ $t('app.logs.levels.info') }}
        </button>
        <button
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: selectedLogLevel === 'warn' }"
          :aria-pressed="selectedLogLevel === 'warn'"
          @click="$emit('set-log-level', 'warn')"
        >
          {{ $t('app.logs.levels.warn') }}
        </button>
        <button
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: selectedLogLevel === 'error' }"
          :aria-pressed="selectedLogLevel === 'error'"
          @click="$emit('set-log-level', 'error')"
        >
          {{ $t('app.logs.levels.error') }}
        </button>
      </div>
    </div>

    <LogEntriesTable :entries="filteredLogs" show-header show-empty />
  </section>
</template>

<style scoped>
.lt-log-levels > .btn {
  --bs-btn-color: var(--lt-muted);
  --bs-btn-bg: var(--lt-surface);
  --bs-btn-border-color: var(--lt-neutral-border);
  --bs-btn-hover-color: var(--lt-ink);
  --bs-btn-hover-bg: var(--lt-surface-soft);
  --bs-btn-hover-border-color: var(--lt-neutral-border);
  --bs-btn-active-color: var(--lt-accent-ink);
  --bs-btn-active-bg: var(--lt-accent-active);
  --bs-btn-active-border-color: var(--lt-accent-border);
  font-weight: 400;
}

.lt-log-levels > .btn.active {
  font-weight: 500;
}

.lt-log-levels > .btn:focus-visible {
  box-shadow: 0 0 0 0.16rem var(--lt-brand-focus-ring);
}
</style>
