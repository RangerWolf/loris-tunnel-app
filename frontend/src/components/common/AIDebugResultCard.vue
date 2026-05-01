<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  state: {
    type: Object,
    required: true
  },
  showActions: {
    type: Boolean,
    default: false
  }
})

const { t } = useI18n()
const emit = defineEmits(['retry-debug', 'test-again'])

const hasResult = computed(() => !!props.state?.result)
const steps = computed(() => Array.isArray(props.state?.result?.steps) ? props.state.result.steps : [])
const checks = computed(() => Array.isArray(props.state?.result?.checks) ? props.state.result.checks : [])
const rules = computed(() => Array.isArray(props.state?.result?.matchedRules) ? props.state.result.matchedRules : [])
const progressSteps = computed(() => [
  t('app.aiDebug.progress.sshClient'),
  t('app.aiDebug.progress.reproduce'),
  t('app.aiDebug.progress.analyze'),
  t('app.aiDebug.progress.suggest')
])
const confidenceLabel = computed(() => {
  const confidence = String(props.state?.result?.confidence || '').toLowerCase()
  if (confidence === 'high') return t('app.aiDebug.confidence.high')
  if (confidence === 'medium') return t('app.aiDebug.confidence.medium')
  if (confidence === 'low') return t('app.aiDebug.confidence.low')
  return props.state?.result?.confidence || ''
})
const confidenceBadgeLabel = computed(() => {
  if (props.state?.result?.usedFallback) return ''
  return confidenceLabel.value ? t('app.aiDebug.confidenceLabel', { value: confidenceLabel.value }) : ''
})
</script>

<template>
  <div v-if="state.status === 'analyzing'" class="ai-debug-card ai-debug-card-pending mt-2">
    <div class="ai-debug-card-head">
      <div>
        <div class="ai-debug-title">{{ $t('app.aiDebug.panelTitle') }}</div>
        <div class="ai-debug-subtitle">{{ $t('app.aiDebug.panelHint') }}</div>
      </div>
      <span class="spinner-border spinner-border-sm text-primary" aria-hidden="true" />
    </div>
    <ol class="ai-debug-progress mt-3 mb-0">
      <li v-for="step in progressSteps" :key="step">{{ step }}</li>
    </ol>
  </div>

  <div v-else-if="state.error" class="ai-debug-card ai-debug-card-error mt-2">
    <div class="ai-debug-card-head">
      <div>
        <div class="ai-debug-title text-danger-emphasis">{{ $t('app.aiDebug.failed') }}</div>
        <div class="ai-debug-subtitle text-danger">{{ state.error }}</div>
      </div>
    </div>
    <div v-if="showActions" class="ai-debug-actions mt-3">
      <button type="button" class="btn btn-sm btn-outline-primary" @click="emit('retry-debug')">
        <i class="bi bi-arrow-repeat" />
        <span>{{ $t('app.aiDebug.retryDebug') }}</span>
      </button>
    </div>
  </div>

  <div v-else-if="hasResult" class="ai-debug-card mt-2">
    <div class="ai-debug-card-head">
      <div class="ai-debug-main">
        <div class="ai-debug-title">{{ $t('app.aiDebug.resultTitle') }}</div>
        <div class="ai-debug-reason">{{ state.result.reason }}</div>
      </div>
      <span v-if="confidenceBadgeLabel" class="badge text-bg-light ai-debug-confidence">{{ confidenceBadgeLabel }}</span>
    </div>

    <div v-if="state.result.summary" class="ai-debug-summary mt-2">{{ state.result.summary }}</div>

    <div v-if="steps.length" class="mt-3">
      <div class="ai-debug-label">{{ $t('app.aiDebug.firstActions') }}</div>
      <ol class="ai-debug-steps mb-0">
        <li v-for="(step, index) in steps" :key="`${index}-${step}`">{{ step }}</li>
      </ol>
    </div>

    <div v-if="state.result.usedFallback" class="small text-muted mt-2">{{ $t('app.aiDebug.fallbackHint') }}</div>

    <div v-if="showActions" class="ai-debug-actions mt-3">
      <button type="button" class="btn btn-sm btn-primary" @click="emit('test-again')">
        <i class="bi bi-arrow-clockwise" />
        <span>{{ $t('app.aiDebug.testAgain') }}</span>
      </button>
      <button type="button" class="btn btn-sm btn-outline-secondary" @click="emit('retry-debug')">
        <i class="bi bi-arrow-repeat" />
        <span>{{ $t('app.aiDebug.retryDebug') }}</span>
      </button>
    </div>

    <details class="ai-debug-details mt-3">
      <summary>{{ t('app.aiDebug.details') }}</summary>
      <div v-if="state.result.sshClientPath" class="mt-2">
        <div class="ai-debug-label">{{ $t('app.aiDebug.sshClient') }}</div>
        <div class="ai-debug-mono">{{ state.result.sshClientPath }}</div>
        <div v-if="state.result.sshVersion" class="ai-debug-mono text-muted">{{ state.result.sshVersion }}</div>
      </div>

      <div v-if="rules.length" class="mt-3">
        <div class="ai-debug-label">{{ $t('app.aiDebug.matchedRules') }}</div>
        <ul class="ai-debug-list mb-0">
          <li v-for="rule in rules" :key="rule">{{ rule }}</li>
        </ul>
      </div>

      <div v-if="checks.length" class="mt-3">
        <div class="ai-debug-label">{{ $t('app.aiDebug.checks') }}</div>
        <ul class="ai-debug-list mb-0">
          <li v-for="check in checks" :key="`${check.name}-${check.detail}`">
            <span class="text-uppercase small fw-semibold">{{ check.status }}</span>
            <span class="mx-1">·</span>
            <span>{{ check.name }}</span>
            <span v-if="check.detail">: {{ check.detail }}</span>
          </li>
        </ul>
      </div>

      <div v-if="state.result.rawError" class="mt-3">
        <div class="ai-debug-label">{{ $t('app.aiDebug.rawError') }}</div>
        <pre class="ai-debug-pre">{{ state.result.rawError }}</pre>
      </div>

      <div v-if="state.result.debugExcerpt" class="mt-3">
        <div class="ai-debug-label">{{ $t('app.aiDebug.debugExcerpt') }}</div>
        <pre class="ai-debug-pre">{{ state.result.debugExcerpt }}</pre>
      </div>
    </details>
  </div>
</template>

<style scoped>
.ai-debug-card {
  border: 1px solid rgba(120, 120, 120, 0.22);
  border-radius: 8px;
  padding: 13px 14px;
  background: rgba(248, 249, 250, 0.9);
  min-height: 180px;
}

.ai-debug-card-pending {
  background: rgba(248, 249, 250, 0.6);
}

.ai-debug-card-error {
  border-color: rgba(220, 53, 69, 0.35);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.ai-debug-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.85rem;
}

.ai-debug-main {
  min-width: 0;
}

.ai-debug-title {
  color: var(--bs-body-color);
  font-size: 0.8rem;
  font-weight: 800;
  line-height: 1.2;
}

.ai-debug-subtitle {
  color: var(--bs-secondary-color);
  font-size: 0.74rem;
  line-height: 1.35;
  margin-top: 0.25rem;
}

.ai-debug-label {
  font-size: 0.72rem;
  letter-spacing: 0;
  text-transform: uppercase;
  color: var(--bs-secondary-color);
  margin-bottom: 0.2rem;
}

.ai-debug-reason {
  font-size: 0.82rem;
  font-weight: 600;
  margin-top: 0.3rem;
}

.ai-debug-summary {
  color: var(--bs-body-color);
  font-size: 0.78rem;
  line-height: 1.42;
}

.ai-debug-steps,
.ai-debug-list {
  padding-left: 1.1rem;
  font-size: 0.78rem;
}

.ai-debug-list li,
.ai-debug-steps li {
  margin-bottom: 0.35rem;
}

.ai-debug-progress {
  display: grid;
  gap: 0.5rem;
  list-style: none;
  padding-left: 0;
}

.ai-debug-progress li {
  position: relative;
  padding-left: 1.5rem;
  color: var(--bs-secondary-color);
  font-size: 0.76rem;
}

.ai-debug-progress li::before {
  position: absolute;
  left: 0;
  top: 0.12rem;
  width: 0.78rem;
  height: 0.78rem;
  border: 2px solid var(--bs-primary);
  border-radius: 50%;
  content: "";
}

.ai-debug-progress li:first-child::before {
  background: var(--bs-primary);
}

.ai-debug-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.ai-debug-actions .btn {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
}

.ai-debug-details summary {
  cursor: pointer;
  color: var(--bs-primary-text-emphasis);
  font-weight: 600;
}

.ai-debug-mono,
.ai-debug-pre {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.8rem;
}

.ai-debug-pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 220px;
  overflow: auto;
  padding: 0.65rem 0.75rem;
  border-radius: 10px;
  background: rgba(33, 37, 41, 0.05);
}

.ai-debug-confidence {
  white-space: nowrap;
}
</style>
