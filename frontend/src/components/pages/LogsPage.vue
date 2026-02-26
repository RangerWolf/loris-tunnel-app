<script setup>
import { getLogLevelClass } from '../../utils/log-level-class'

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
      <div class="btn-group btn-group-sm">
        <button
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: selectedLogLevel === 'all' }"
          @click="$emit('set-log-level', 'all')"
        >
          {{ $t('app.logs.levels.all') }}
        </button>
        <button
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: selectedLogLevel === 'info' }"
          @click="$emit('set-log-level', 'info')"
        >
          {{ $t('app.logs.levels.info') }}
        </button>
        <button
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: selectedLogLevel === 'warn' }"
          @click="$emit('set-log-level', 'warn')"
        >
          {{ $t('app.logs.levels.warn') }}
        </button>
        <button
          type="button"
          class="btn btn-outline-secondary"
          :class="{ active: selectedLogLevel === 'error' }"
          @click="$emit('set-log-level', 'error')"
        >
          {{ $t('app.logs.levels.error') }}
        </button>
      </div>
    </div>

    <div class="table-responsive">
      <table class="table align-middle mb-0 logs-table">
        <thead>
          <tr>
            <th class="logs-time-col">{{ $t('app.logs.table.time') }}</th>
            <th class="logs-level-col">{{ $t('app.logs.table.level') }}</th>
            <th>{{ $t('app.logs.table.message') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="filteredLogs.length === 0">
            <td colspan="3" class="text-muted py-4">{{ $t('app.logs.noLogs') }}</td>
          </tr>
          <tr v-for="entry in filteredLogs" :key="entry.id">
            <td class="text-muted logs-time-cell">
              <span class="cell-ellipsis" :title="entry.time">{{ entry.time }}</span>
            </td>
            <td>
              <span class="status-badge" :class="getLogLevelClass(entry.level, 'statusBadge')">
                {{ entry.level.toUpperCase() }}
              </span>
            </td>
            <td>{{ entry.message }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
