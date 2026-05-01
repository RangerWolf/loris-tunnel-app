<script setup>
import AIDebugResultCard from './AIDebugResultCard.vue'

defineProps({
  show: {
    type: Boolean,
    required: true
  },
  title: {
    type: String,
    required: true
  },
  subtitle: {
    type: String,
    default: ''
  },
  rawError: {
    type: String,
    default: ''
  },
  state: {
    type: Object,
    required: true
  }
})

defineEmits(['close', 'retry-debug', 'test-again'])
</script>

<template>
  <div v-if="show" class="overlay">
    <div class="dialog-card compact-dialog ai-debug-dialog">
      <div class="dialog-head ai-debug-dialog-head">
        <div>
          <h3 class="dialog-title">{{ title }}</h3>
          <div v-if="subtitle" class="ai-debug-dialog-subtitle">{{ subtitle }}</div>
        </div>
        <button type="button" class="btn-close" :aria-label="$t('app.common.close')" @click="$emit('close')" />
      </div>
      <div class="dialog-body">
        <div v-if="rawError" class="ai-debug-source-error">
          <div class="ai-debug-source-label">{{ $t('app.aiDebug.sourceError') }}</div>
          <div class="ai-debug-source-message">{{ rawError }}</div>
        </div>
        <AIDebugResultCard
          :state="state"
          show-actions
          @retry-debug="$emit('retry-debug')"
          @test-again="$emit('test-again')"
        />
      </div>
      <div class="dialog-actions ai-debug-dialog-actions">
        <button type="button" class="btn btn-outline-secondary" @click="$emit('close')">
          {{ $t('app.common.close') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ai-debug-dialog {
  width: min(720px, 100%);
}

.ai-debug-dialog-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}

.ai-debug-dialog-subtitle {
  color: var(--bs-secondary-color);
  font-size: 0.8rem;
  margin-top: 0.25rem;
}

.ai-debug-source-error {
  border: 1px solid rgba(220, 53, 69, 0.22);
  border-radius: 8px;
  background: var(--lt-danger-bg);
  padding: 0.75rem 0.85rem;
}

.ai-debug-source-label {
  color: var(--lt-danger-ink);
  font-size: 0.72rem;
  font-weight: 800;
  letter-spacing: 0;
  text-transform: uppercase;
  margin-bottom: 0.25rem;
}

.ai-debug-source-message {
  color: var(--lt-danger-ink);
  font-size: 0.82rem;
  line-height: 1.38;
  white-space: pre-wrap;
  word-break: break-word;
}

.ai-debug-dialog-actions {
  justify-content: flex-end;
  margin-top: 0;
  padding: 0.85rem 1rem;
  border-top: 1px solid var(--lt-border);
}
</style>
