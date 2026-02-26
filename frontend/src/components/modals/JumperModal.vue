<script setup>
import { onMounted, onUpdated, onBeforeUnmount, ref } from 'vue'
import { Tooltip } from 'bootstrap'

defineProps({
  show: {
    type: Boolean,
    required: true
  },
  editingJumperId: {
    type: Number,
    default: null
  },
  jumperForm: {
    type: Object,
    required: true
  },
  showJumperBasic: {
    type: Boolean,
    required: true
  },
  showJumperAdvanced: {
    type: Boolean,
    required: true
  },
  authOptions: {
    type: Array,
    required: true
  },
  jumperNeedsKeyFile: {
    type: Boolean,
    required: true
  },
  jumperNeedsPassword: {
    type: Boolean,
    required: true
  },
  jumperShowsPassword: {
    type: Boolean,
    required: true
  },
  jumperLimits: {
    type: Object,
    required: true
  },
  jumperValidationError: {
    type: String,
    default: ''
  },
  jumperTest: {
    type: Object,
    required: true
  }
})

defineEmits(['close', 'submit', 'toggle-basic', 'toggle-advanced', 'key-file-change', 'test-connection'])

const keepAliveHintRef = ref(null)
const bypassHintRef = ref(null)
let keepAliveTooltip = null
let bypassTooltip = null

function syncKeepAliveTooltip() {
  if (!keepAliveHintRef.value) {
    if (keepAliveTooltip) {
      keepAliveTooltip.dispose()
      keepAliveTooltip = null
    }
    return
  }

  if (!keepAliveTooltip) {
    keepAliveTooltip = new Tooltip(keepAliveHintRef.value)
  }
  keepAliveTooltip.setContent({ '.tooltip-inner': keepAliveHintRef.value.getAttribute('data-bs-title') || '' })
}

function syncBypassTooltip() {
  if (!bypassHintRef.value) {
    if (bypassTooltip) {
      bypassTooltip.dispose()
      bypassTooltip = null
    }
    return
  }

  if (!bypassTooltip) {
    bypassTooltip = new Tooltip(bypassHintRef.value)
  }
  bypassTooltip.setContent({ '.tooltip-inner': bypassHintRef.value.getAttribute('data-bs-title') || '' })
}

function syncTooltips() {
  syncKeepAliveTooltip()
  syncBypassTooltip()
}

function disposeTooltips() {
  if (keepAliveTooltip) {
    keepAliveTooltip.dispose()
    keepAliveTooltip = null
  }
  if (bypassTooltip) {
    bypassTooltip.dispose()
    bypassTooltip = null
  }
}

onMounted(() => {
  syncTooltips()
})

onUpdated(() => {
  syncTooltips()
})

onBeforeUnmount(() => {
  disposeTooltips()
})
</script>

<template>
  <div v-if="show" class="overlay" @click.self="$emit('close')">
    <div class="dialog-card compact-dialog jumper-dialog">
      <div class="dialog-head">
        <h3 class="dialog-title">{{ editingJumperId ? $t('app.modals.jumper.editTitle') : $t('app.modals.jumper.newTitle') }}</h3>
      </div>
      <form
        class="dialog-body"
        autocapitalize="none"
        autocorrect="off"
        spellcheck="false"
        @submit.prevent="$emit('submit')"
      >
        <div class="row g-2">
          <div class="col-md-12">
            <button type="button" class="btn advanced-toggle px-0" @click="$emit('toggle-basic')">
              <span class="advanced-chevron" :class="{ open: showJumperBasic }">▸</span>
              {{ $t('app.modals.jumper.basicSettings') }}
            </button>
          </div>

          <div v-if="showJumperBasic" class="col-md-12">
            <div class="advanced-box">
              <div class="row g-2">
                <div class="col-md-6">
                  <label class="form-label">{{ $t('app.modals.jumper.name') }}</label>
                  <input
                    v-model="jumperForm.name"
                    class="form-control"
                    type="text"
                    autocapitalize="none"
                    autocorrect="off"
                    spellcheck="false"
                    :maxlength="jumperLimits.name"
                    required
                  />
                </div>
                <div class="col-md-6">
                  <label class="form-label">{{ $t('app.modals.jumper.user') }}</label>
                  <input
                    v-model="jumperForm.user"
                    class="form-control"
                    type="text"
                    autocapitalize="none"
                    autocorrect="off"
                    spellcheck="false"
                    :maxlength="jumperLimits.user"
                    required
                  />
                </div>
                <div class="col-md-8">
                  <label class="form-label">{{ $t('app.modals.jumper.host') }}</label>
                  <input
                    v-model="jumperForm.host"
                    class="form-control"
                    type="text"
                    autocapitalize="none"
                    autocorrect="off"
                    spellcheck="false"
                    :maxlength="jumperLimits.host"
                    required
                  />
                </div>
                <div class="col-md-4">
                  <label class="form-label">{{ $t('app.modals.jumper.port') }}</label>
                  <input v-model.number="jumperForm.port" class="form-control" type="number" min="1" max="65535" required />
                </div>
                <div class="col-md-6">
                  <label class="form-label">{{ $t('app.modals.jumper.authMethod') }}</label>
                  <select v-model="jumperForm.authType" class="form-select">
                    <option v-for="option in authOptions" :key="option.value" :value="option.value">
                      {{ option.label }}
                    </option>
                  </select>
                  <div v-if="jumperForm.authType === 'ssh_agent'" class="field-note mt-1">
                    {{ $t('app.modals.jumper.sshAgentNote') }}
                  </div>
                </div>
                <div v-if="jumperForm.authType === 'ssh_agent'" class="col-md-6">
                  <label class="form-label">{{ $t('app.modals.jumper.agentSocketPath') }}</label>
                  <input
                    v-model="jumperForm.agentSocketPath"
                    class="form-control"
                    type="text"
                    autocapitalize="none"
                    autocorrect="off"
                    spellcheck="false"
                    :maxlength="jumperLimits.agentSocketPath"
                    :placeholder="$t('app.modals.jumper.agentSocketPlaceholder')"
                  />
                  <div class="field-note">{{ $t('app.modals.jumper.agentSocketNote') }}</div>
                </div>

                <template v-if="jumperNeedsKeyFile">
                  <div class="col-md-7">
                    <label class="form-label">{{ $t('app.modals.jumper.sshKeyFile') }}</label>
                    <div class="input-group">
                      <input
                        v-model="jumperForm.keyPath"
                        class="form-control"
                        type="text"
                        autocapitalize="none"
                        autocorrect="off"
                        spellcheck="false"
                        :maxlength="jumperLimits.keyPath"
                        :placeholder="$t('app.modals.jumper.keyPathPlaceholder')"
                        :required="jumperNeedsKeyFile"
                      />
                      <label class="btn btn-outline-secondary mb-0">
                        {{ $t('app.modals.jumper.browse') }}
                        <input class="d-none" type="file" @change="$emit('key-file-change', $event)" />
                      </label>
                    </div>
                    <div class="field-note">{{ $t('app.modals.jumper.keyFileNote') }}</div>
                  </div>
                  <div class="col-md-5">
                    <label class="form-label">{{ $t('app.modals.jumper.password') }}</label>
                    <input
                      v-model="jumperForm.password"
                      class="form-control"
                      type="password"
                      autocapitalize="none"
                      autocorrect="off"
                      spellcheck="false"
                      :maxlength="jumperLimits.password"
                      :placeholder="$t('app.modals.jumper.passwordOptionalPlaceholder')"
                      :required="jumperNeedsPassword"
                    />
                  </div>
                </template>

                <div v-else-if="jumperShowsPassword" class="col-md-12">
                  <label class="form-label">{{ $t('app.modals.jumper.password') }}</label>
                  <input
                    v-model="jumperForm.password"
                    class="form-control"
                    type="password"
                    autocapitalize="none"
                    autocorrect="off"
                    spellcheck="false"
                    :maxlength="jumperLimits.password"
                    :placeholder="$t('app.modals.jumper.passwordPlaceholder')"
                    :required="jumperNeedsPassword"
                  />
                </div>

                <div class="col-md-12">
                  <label class="form-label">{{ $t('app.modals.jumper.notes') }}</label>
                  <textarea
                    v-model="jumperForm.notes"
                    class="form-control"
                    rows="2"
                    autocapitalize="none"
                    autocorrect="off"
                    spellcheck="false"
                    :maxlength="jumperLimits.notes"
                  />
                </div>
              </div>
            </div>
          </div>

          <div class="col-md-12">
            <button type="button" class="btn advanced-toggle px-0" @click="$emit('toggle-advanced')">
              <span class="advanced-chevron" :class="{ open: showJumperAdvanced }">▸</span>
              {{ $t('app.modals.jumper.advancedOptions') }}
            </button>
          </div>

          <div v-if="showJumperAdvanced" class="col-md-12">
            <div class="advanced-box">
              <div class="row g-3">
                <div class="col-md-6">
                  <div class="d-flex align-items-center gap-2">
                    <label class="form-label mb-0">{{ $t('app.modals.jumper.keepAliveInterval') }}</label>
                    <span
                      ref="keepAliveHintRef"
                      class="hint-dot"
                      data-bs-toggle="tooltip"
                      data-bs-placement="top"
                      :data-bs-title="$t('app.modals.jumper.keepAliveIntervalTooltip')"
                    >?</span>
                  </div>
                  <input
                    v-model.number="jumperForm.keepAliveIntervalMs"
                    class="form-control"
                    type="number"
                    :min="jumperLimits.keepAliveIntervalMin"
                    :max="jumperLimits.keepAliveIntervalMax"
                    step="1000"
                    required
                  />
                </div>
                <div class="col-md-6">
                  <label class="form-label">{{ $t('app.modals.jumper.timeout') }}</label>
                  <input
                    v-model.number="jumperForm.timeoutMs"
                    class="form-control"
                    type="number"
                    :min="jumperLimits.timeoutMin"
                    :max="jumperLimits.timeoutMax"
                    step="100"
                    required
                  />
                </div>
                <div class="col-md-6">
                  <div class="d-flex align-items-center gap-2 pt-1">
                    <div class="form-check form-switch m-0">
                      <input
                        id="bypassHostSwitch"
                        v-model="jumperForm.bypassHostVerification"
                        class="form-check-input"
                        type="checkbox"
                      />
                      <label class="form-check-label" for="bypassHostSwitch">{{ $t('app.modals.jumper.bypassHostCheck') }}</label>
                    </div>
                    <span
                      ref="bypassHintRef"
                      class="hint-dot"
                      data-bs-toggle="tooltip"
                      data-bs-placement="top"
                      :data-bs-title="$t('app.modals.jumper.bypassTooltip')"
                    >i</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <p v-if="jumperValidationError" class="form-error mb-0 mt-3">{{ jumperValidationError }}</p>

        <div class="dialog-actions mt-4">
          <div class="dialog-left-actions">
            <button
              type="button"
              class="btn btn-outline-primary"
              :disabled="jumperTest.status === 'testing'"
              @click="$emit('test-connection')"
            >
              {{ jumperTest.status === 'testing' ? $t('app.modals.jumper.testing') : $t('app.modals.jumper.testConnection') }}
            </button>
            <span
              v-if="jumperTest.message && jumperTest.status !== 'error'"
              class="test-result"
              :class="{ success: jumperTest.status === 'success' }"
            >
              {{ jumperTest.message }}
            </span>
          </div>
          <div class="dialog-right-actions">
            <button type="button" class="btn btn-outline-secondary" @click="$emit('close')">{{ $t('app.common.cancel') }}</button>
            <button type="submit" class="btn btn-primary">{{ $t('app.common.save') }}</button>
          </div>
        </div>
        <div
          v-if="jumperTest.message && jumperTest.status === 'error'"
          class="test-result error mt-2"
        >
          {{ jumperTest.message }}
        </div>
      </form>
    </div>
  </div>
</template>
