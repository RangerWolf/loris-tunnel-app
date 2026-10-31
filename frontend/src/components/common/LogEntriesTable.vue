<script setup>
import TooltipText from './TooltipText.vue'
import { getLogLevelClass } from '../../utils/log-level-class'

defineProps({
  entries: {
    type: Array,
    required: true
  },
  showHeader: {
    type: Boolean,
    default: false
  },
  showEmpty: {
    type: Boolean,
    default: false
  }
})
</script>

<template>
  <div class="table-responsive">
    <table class="table align-middle mb-0 logs-table">
      <colgroup>
        <col class="logs-time-col" />
        <col class="logs-level-col" />
        <col />
      </colgroup>
      <thead v-if="showHeader">
        <tr>
          <th class="logs-time-col">{{ $t('app.logs.table.time') }}</th>
          <th class="logs-level-col">{{ $t('app.logs.table.level') }}</th>
          <th>{{ $t('app.logs.table.message') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="showEmpty && entries.length === 0">
          <td colspan="3" class="text-muted py-4">{{ $t('app.logs.noLogs') }}</td>
        </tr>
        <tr v-for="entry in entries" :key="entry.id">
          <td class="text-muted logs-time-cell">
            <TooltipText :text="entry.time" />
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
</template>
