<script setup>
import { computed, onBeforeUnmount, onMounted, onUpdated, ref, watch } from 'vue'
import { Tooltip } from 'bootstrap'

const props = defineProps({
  show: {
    type: Boolean,
    required: true
  },
  editingTunnelId: {
    type: Number,
    default: null
  },
  tunnelForm: {
    type: Object,
    required: true
  },
  modeOptions: {
    type: Array,
    required: true
  },
  jumpers: {
    type: Array,
    required: true
  },
  inlineJumperForm: {
    type: Object,
    required: true
  },
  authOptions: {
    type: Array,
    required: true
  },
  inlineJumperNeedsKeyFile: {
    type: Boolean,
    required: true
  },
  inlineJumperNeedsPassword: {
    type: Boolean,
    required: true
  },
  inlineJumperShowsPassword: {
    type: Boolean,
    required: true
  },
  jumperLimits: {
    type: Object,
    required: true
  },
  inlineJumperValidationError: {
    type: String,
    default: ''
  },
  tunnelValidationError: {
    type: String,
    default: ''
  },
  tunnelTest: {
    type: Object,
    required: true
  }
})

const emit = defineEmits([
  'close',
  'submit',
  'add-jumper',
  'remove-jumper',
  'set-primary-jumper',
  'move-jumper',
  'trim-jumpers-to-primary',
  'inline-key-file-change',
  'test-connection'
])

const selectedJumpersHintRef = ref(null)
const showMoreJumpers = ref(false)
let selectedJumpersTooltip = null

const selectedJumperIds = computed(() => {
  const rawIds = Array.isArray(props.tunnelForm?.jumperIds) ? props.tunnelForm.jumperIds : []
  const ids = []
  for (const value of rawIds) {
    const id = Number(value)
    if (!Number.isInteger(id) || id <= 0 || ids.includes(id)) continue
    ids.push(id)
  }
  return ids
})

const primaryJumperId = computed(() => {
  return selectedJumperIds.value.length > 0 ? selectedJumperIds.value[0] : ''
})

const additionalSelectedJumperIds = computed(() => {
  return selectedJumperIds.value.slice(1)
})

const availableAdditionalJumpers = computed(() => {
  const selectedSet = new Set(selectedJumperIds.value)
  return (Array.isArray(props.jumpers) ? props.jumpers : []).filter((jumper) => !selectedSet.has(Number(jumper.id)))
})

const showJumperChainEditor = computed(() => showMoreJumpers.value)

watch(
  () => props.show,
  (visible) => {
    if (!visible) {
      showMoreJumpers.value = false
      return
    }
    showMoreJumpers.value = additionalSelectedJumperIds.value.length > 0
  }
)

watch(showMoreJumpers, (enabled) => {
  if (enabled) return
  if (!props.show) return
  if (selectedJumperIds.value.length <= 1) return
  emit('trim-jumpers-to-primary')
})

function syncSelectedJumpersTooltip() {
  if (!selectedJumpersHintRef.value) {
    if (selectedJumpersTooltip) {
      selectedJumpersTooltip.dispose()
      selectedJumpersTooltip = null
    }
    return
  }

  if (!selectedJumpersTooltip) {
    selectedJumpersTooltip = new Tooltip(selectedJumpersHintRef.value)
  }
  selectedJumpersTooltip.setContent({ '.tooltip-inner': selectedJumpersHintRef.value.getAttribute('data-bs-title') || '' })
}

function disposeTooltips() {
  if (selectedJumpersTooltip) {
    selectedJumpersTooltip.dispose()
    selectedJumpersTooltip = null
  }
}

onMounted(() => {
  syncSelectedJumpersTooltip()
})

onUpdated(() => {
  syncSelectedJumpersTooltip()
})

onBeforeUnmount(() => {
  disposeTooltips()
})

function getJumperById(jumperId) {
  const id = Number(jumperId)
  if (!Number.isInteger(id) || id <= 0) return null
  return (Array.isArray(props.jumpers) ? props.jumpers : []).find((item) => Number(item.id) === id) || null
}

function getJumperDisplayName(jumperId) {
  const jumper = getJumperById(jumperId)
  if (!jumper) return `#${jumperId}`
  return String(jumper.name || '').trim() || `#${jumperId}`
}

function getJumperConnectionLabel(jumperId) {
  const jumper = getJumperById(jumperId)
  if (!jumper) return ''
  return `${jumper.user}@${jumper.host}`
}

function getJumperTooltipLabel(jumperId) {
  const jumper = getJumperById(jumperId)
  if (!jumper) return `#${jumperId}`
  return `${jumper.name} (${jumper.user}@${jumper.host})`
}

function onPrimaryJumperChange(event) {
  const nextId = Number(event?.target?.value)
  if (!Number.isInteger(nextId) || nextId <= 0) return
  emit('set-primary-jumper', nextId)
}
</script>

<template>
  <div v-if="show" class="overlay" @click.self="$emit('close')">
    <div class="dialog-card dialog-large compact-dialog tunnel-dialog">
      <div class="dialog-head">
        <h3 class="dialog-title">{{ editingTunnelId ? $t('app.modals.tunnel.editTitle') : $t('app.modals.tunnel.newTitle') }}</h3>
      </div>
      <form
        class="dialog-body"
        autocapitalize="none"
        autocorrect="off"
        spellcheck="false"
        @submit.prevent="$emit('submit')"
      >
        <div class="row g-3">
          <div class="col-md-6">
            <label class="form-label">{{ $t('app.modals.tunnel.name') }}</label>
            <input
              v-model="tunnelForm.name"
              class="form-control"
              type="text"
              autocapitalize="none"
              autocorrect="off"
              spellcheck="false"
              maxlength="20"
              required
            />
          </div>
          <div class="col-md-6">
            <label class="form-label">{{ $t('app.modals.tunnel.mode') }}</label>
            <select v-model="tunnelForm.mode" class="form-select">
              <option v-for="mode in modeOptions" :key="mode.value" :value="mode.value">
                {{ mode.label }}
              </option>
            </select>
          </div>
          <div class="col-md-6">
            <div class="tunnel-endpoint-grid">
              <div>
                <label class="form-label">{{ $t('app.modals.tunnel.localHost') }}</label>
                <input
                  v-model="tunnelForm.localHost"
                  class="form-control"
                  type="text"
                  autocapitalize="none"
                  autocorrect="off"
                  spellcheck="false"
                  required
                />
              </div>
              <div class="tunnel-port-field">
                <label class="form-label">{{ $t('app.modals.tunnel.localPort') }}</label>
                <input v-model.number="tunnelForm.localPort" class="form-control" type="number" min="1" required />
              </div>
            </div>
          </div>
          <div class="col-md-6">
            <div class="tunnel-endpoint-grid">
              <div>
                <label class="form-label">{{ $t('app.modals.tunnel.remoteHost') }}</label>
                <input
                  v-model="tunnelForm.remoteHost"
                  class="form-control"
                  type="text"
                  autocapitalize="none"
                  autocorrect="off"
                  spellcheck="false"
                  :disabled="tunnelForm.mode === 'dynamic'"
                  :required="tunnelForm.mode !== 'dynamic'"
                />
              </div>
              <div class="tunnel-port-field">
                <label class="form-label">{{ $t('app.modals.tunnel.remotePort') }}</label>
                <input
                  v-model.number="tunnelForm.remotePort"
                  class="form-control"
                  type="number"
                  min="1"
                  :disabled="tunnelForm.mode === 'dynamic'"
                  :required="tunnelForm.mode !== 'dynamic'"
                />
              </div>
            </div>
          </div>
          <div class="col-md-12">
            <label class="form-label">{{ $t('app.modals.tunnel.jumpers') }}</label>
            <div class="jumper-primary-row">
              <span class="jumper-primary-index">#1</span>
              <select
                class="form-select jumper-primary-select"
                :value="primaryJumperId"
                :disabled="jumpers.length === 0"
                @change="onPrimaryJumperChange"
              >
                <option value="" disabled>{{ $t('app.modals.tunnel.selectPrimaryJumperPlaceholder') }}</option>
                <option
                  v-for="jumper in jumpers"
                  :key="jumper.id"
                  :value="jumper.id"
                  :title="`${jumper.name} (${jumper.user}@${jumper.host})`"
                >
                  {{ jumper.name }} ({{ jumper.user }}@{{ jumper.host }})
                </option>
              </select>
            </div>
            <div class="field-note mt-1">{{ $t('app.modals.tunnel.primaryJumperHint') }}</div>
            <div class="form-check form-switch mt-2">
              <input
                id="addMoreJumpersSwitch"
                v-model="showMoreJumpers"
                class="form-check-input"
                type="checkbox"
                :disabled="jumpers.length === 0"
                :aria-expanded="showJumperChainEditor"
              />
              <label for="addMoreJumpersSwitch" class="form-check-label">{{ $t('app.modals.tunnel.addMoreJumpers') }}</label>
            </div>
          </div>
          <div v-if="showJumperChainEditor" class="col-md-12">
            <div class="jumper-chain-editor">
              <div class="d-flex align-items-center gap-2">
                <div class="form-label mb-2">{{ $t('app.modals.tunnel.selectedJumpers') }}</div>
                <span
                  ref="selectedJumpersHintRef"
                  class="hint-dot mb-2"
                  data-bs-toggle="tooltip"
                  data-bs-placement="top"
                  :data-bs-title="$t('app.modals.tunnel.selectedJumpersOrderTooltip')"
                >?</span>
              </div>
              <div class="input-group">
                <select v-model="tunnelForm.nextJumperId" class="form-select" :disabled="availableAdditionalJumpers.length === 0">
                  <option value="" disabled>{{ $t('app.modals.tunnel.selectAdditionalJumperPlaceholder') }}</option>
                  <option
                    v-for="jumper in availableAdditionalJumpers"
                    :key="jumper.id"
                    :value="jumper.id"
                    :title="`${jumper.name} (${jumper.user}@${jumper.host})`"
                  >
                    {{ jumper.name }} ({{ jumper.user }}@{{ jumper.host }})
                  </option>
                </select>
                <button
                  type="button"
                  class="btn btn-outline-primary"
                  :disabled="!tunnelForm.nextJumperId"
                  :aria-label="$t('app.modals.tunnel.addJumper')"
                  @click="$emit('add-jumper', tunnelForm.nextJumperId)"
                >
                  <i class="bi bi-plus-lg" />
                </button>
              </div>
              <div class="field-note mt-1">{{ $t('app.modals.tunnel.jumpersHint') }}</div>
              <div v-if="selectedJumperIds.length === 0" class="text-muted selected-jumper-empty">
                {{ $t('app.modals.tunnel.noSelectedJumpers') }}
              </div>
              <div v-else class="list-group selected-jumper-list mt-2">
                <div
                  v-for="(jumperId, index) in selectedJumperIds"
                  :key="`selected-jumper-${index}-${jumperId}`"
                  class="list-group-item py-2 selected-jumper-item"
                >
                  <div class="selected-jumper-main">
                    <span class="badge text-bg-light">{{ index + 1 }}</span>
                    <div class="selected-jumper-text-wrap" :title="getJumperTooltipLabel(jumperId)">
                      <div class="selected-jumper-name cell-ellipsis">
                        {{
                          getJumperById(jumperId)
                            ? getJumperDisplayName(jumperId)
                            : `${$t('app.options.jumper.unknown')} (#${jumperId})`
                        }}
                      </div>
                      <div v-if="getJumperById(jumperId)" class="text-muted selected-jumper-conn cell-ellipsis">
                        {{ getJumperConnectionLabel(jumperId) }}
                      </div>
                    </div>
                  </div>
                  <div class="selected-jumper-actions">
                    <button
                      type="button"
                      class="btn btn-sm btn-outline-secondary selected-jumper-order-btn"
                      :disabled="index === 0"
                      :aria-label="$t('app.modals.tunnel.moveJumperUp')"
                      @click="$emit('move-jumper', index, -1)"
                    >
                      <i class="bi bi-arrow-up" />
                    </button>
                    <button
                      type="button"
                      class="btn btn-sm btn-outline-secondary selected-jumper-order-btn"
                      :disabled="index === selectedJumperIds.length - 1"
                      :aria-label="$t('app.modals.tunnel.moveJumperDown')"
                      @click="$emit('move-jumper', index, 1)"
                    >
                      <i class="bi bi-arrow-down" />
                    </button>
                    <button
                      type="button"
                      class="btn btn-sm btn-outline-danger selected-jumper-remove-btn"
                      :aria-label="$t('app.modals.tunnel.removeJumper')"
                      @click="$emit('remove-jumper', index)"
                    >
                      <i class="bi bi-trash3" />
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="col-md-12">
            <div class="form-check form-switch">
              <input id="appendNewJumperSwitch" v-model="tunnelForm.appendNewJumper" class="form-check-input" type="checkbox" />
              <label for="appendNewJumperSwitch" class="form-check-label">{{ $t('app.modals.tunnel.appendNewJumper') }}</label>
            </div>
          </div>
        </div>

        <div v-if="tunnelForm.appendNewJumper" class="inline-jumper-block mt-3">
          <div class="block-title">{{ $t('app.modals.tunnel.quickCreateJumper') }}</div>
          <div class="row g-2 mt-0 inline-jumper-form-grid">
            <div class="col-md-6">
              <label class="form-label">{{ $t('app.modals.jumper.name') }}</label>
              <input
                v-model="inlineJumperForm.name"
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
                v-model="inlineJumperForm.user"
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
                v-model="inlineJumperForm.host"
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
              <input v-model.number="inlineJumperForm.port" class="form-control" type="number" min="1" max="65535" required />
            </div>
            <div class="col-md-6">
              <label class="form-label">{{ $t('app.modals.jumper.authMethod') }}</label>
              <select v-model="inlineJumperForm.authType" class="form-select">
                <option v-for="option in authOptions" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
              <div v-if="inlineJumperForm.authType === 'ssh_agent'" class="field-note mt-1">
                {{ $t('app.modals.jumper.sshAgentNote') }}
              </div>
            </div>
            <div v-if="inlineJumperForm.authType === 'ssh_agent'" class="col-md-6">
              <label class="form-label">{{ $t('app.modals.jumper.agentSocketPath') }}</label>
              <input
                v-model="inlineJumperForm.agentSocketPath"
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
            <template v-if="inlineJumperNeedsKeyFile">
              <div class="col-md-7">
                <label class="form-label">{{ $t('app.modals.jumper.sshKeyFile') }}</label>
                <div class="input-group">
                  <input
                    v-model="inlineJumperForm.keyPath"
                    class="form-control"
                    type="text"
                    autocapitalize="none"
                    autocorrect="off"
                    spellcheck="false"
                    :maxlength="jumperLimits.keyPath"
                    :placeholder="$t('app.modals.jumper.keyPathPlaceholder')"
                    :required="inlineJumperNeedsKeyFile"
                  />
                  <label class="btn btn-outline-secondary mb-0">
                    {{ $t('app.modals.jumper.browse') }}
                    <input class="d-none" type="file" @change="$emit('inline-key-file-change', $event)" />
                  </label>
                </div>
                <div class="field-note">{{ $t('app.modals.jumper.keyFileNote') }}</div>
              </div>
              <div class="col-md-5">
                <label class="form-label">{{ $t('app.modals.jumper.password') }}</label>
                <input
                  v-model="inlineJumperForm.password"
                  class="form-control"
                  type="password"
                  autocapitalize="none"
                  autocorrect="off"
                  spellcheck="false"
                  :maxlength="jumperLimits.password"
                  :placeholder="$t('app.modals.jumper.passwordOptionalPlaceholder')"
                  :required="inlineJumperNeedsPassword"
                />
              </div>
            </template>
            <div v-else-if="inlineJumperShowsPassword" class="col-md-12">
              <label class="form-label">{{ $t('app.modals.jumper.password') }}</label>
              <input
                v-model="inlineJumperForm.password"
                class="form-control"
                type="password"
                autocapitalize="none"
                autocorrect="off"
                spellcheck="false"
                :maxlength="jumperLimits.password"
                :placeholder="$t('app.modals.jumper.passwordPlaceholder')"
                :required="inlineJumperNeedsPassword"
              />
            </div>
          </div>
          <p v-if="inlineJumperValidationError" class="form-error mb-0 mt-3">{{ inlineJumperValidationError }}</p>
        </div>

        <div class="row g-3 mt-1">
          <div class="col-md-12">
            <label class="form-label">{{ $t('app.modals.tunnel.description') }}</label>
            <textarea
              v-model="tunnelForm.description"
              class="form-control"
              rows="2"
              autocapitalize="none"
              autocorrect="off"
              spellcheck="false"
            />
          </div>
          <div class="col-md-12">
            <div class="form-check form-switch">
              <input id="autoStartSwitch" v-model="tunnelForm.autoStart" class="form-check-input" type="checkbox" />
              <label for="autoStartSwitch" class="form-check-label">{{ $t('app.modals.tunnel.autoStart') }}</label>
            </div>
          </div>
        </div>

        <p v-if="tunnelValidationError" class="form-error mb-0 mt-3">{{ tunnelValidationError }}</p>

        <div class="dialog-actions mt-4">
          <div class="dialog-left-actions">
            <button
              type="button"
              class="btn btn-outline-primary"
              :disabled="tunnelTest.status === 'testing'"
              @click="$emit('test-connection')"
            >
              {{ tunnelTest.status === 'testing' ? $t('app.modals.tunnel.testing') : $t('app.modals.tunnel.testConnection') }}
            </button>
            <span
              v-if="tunnelTest.message && tunnelTest.status !== 'error'"
              class="test-result"
              :class="{ success: tunnelTest.status === 'success' }"
            >
              {{ tunnelTest.message }}
            </span>
          </div>
          <div class="dialog-right-actions">
            <button type="button" class="btn btn-outline-secondary" @click="$emit('close')">{{ $t('app.common.cancel') }}</button>
            <button type="submit" class="btn btn-primary">{{ $t('app.common.save') }}</button>
          </div>
        </div>
        <div
          v-if="tunnelTest.message && tunnelTest.status === 'error'"
          class="dialog-test-result test-result error mt-2"
        >
          {{ tunnelTest.message }}
        </div>
      </form>
    </div>
  </div>
</template>
