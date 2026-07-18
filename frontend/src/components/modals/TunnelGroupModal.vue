<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import IconActionButton from '../common/IconActionButton.vue'

const props = defineProps({
  show: {
    type: Boolean,
    required: true
  },
  groups: {
    type: Array,
    default: () => []
  },
  hideEmptyUngrouped: {
    type: Boolean,
    default: true
  },
  initialEditGroupId: {
    type: Number,
    default: null
  },
  errorMessage: {
    type: String,
    default: ''
  },
  nameMaxLength: {
    type: Number,
    default: 20
  }
})

const emit = defineEmits([
  'close',
  'create-group',
  'rename-group',
  'delete-group',
  'reorder-groups',
  'update:hide-empty-ungrouped'
])

const { t } = useI18n()
const newGroupName = ref('')
const editingGroupId = ref(null)
const editingGroupName = ref('')
const localValidationError = ref('')
const orderedGroups = ref([])
const draggingGroupId = ref(null)
const dragOverGroupId = ref(null)

const displayError = computed(() => localValidationError.value || props.errorMessage)

function syncOrderedGroups(groups) {
  orderedGroups.value = (Array.isArray(groups) ? groups : []).map((group) => ({ ...group }))
}

watch(
  () => props.groups,
  (groups) => {
    if (draggingGroupId.value != null) return
    syncOrderedGroups(groups)
  },
  { immediate: true, deep: true }
)

watch(
  () => props.show,
  (visible) => {
    if (!visible) {
      newGroupName.value = ''
      editingGroupId.value = null
      editingGroupName.value = ''
      localValidationError.value = ''
      draggingGroupId.value = null
      dragOverGroupId.value = null
      return
    }
    syncOrderedGroups(props.groups)
    if (props.initialEditGroupId) {
      const group = props.groups.find((item) => Number(item.id) === Number(props.initialEditGroupId))
      if (group) {
        startRename(group)
      }
    }
  }
)

watch([newGroupName, editingGroupName], () => {
  localValidationError.value = ''
})

function nameUnits(text) {
  let units = 0
  for (const char of text || '') {
    units += /[\u3400-\u9fff\uf900-\ufaff]/.test(char) ? 2 : 1
  }
  return units
}

function isDuplicateGroupName(name, excludeId = null) {
  const normalized = String(name || '').trim().toLowerCase()
  if (!normalized) return false
  return props.groups.some((group) => {
    if (excludeId != null && Number(group.id) === Number(excludeId)) return false
    return String(group.name || '').trim().toLowerCase() === normalized
  })
}

function validateGroupName(name, excludeId = null) {
  const trimmed = String(name || '').trim()
  if (!trimmed) return t('app.tunnels.groups.nameRequired')
  if (nameUnits(trimmed) > props.nameMaxLength) {
    return t('app.tunnels.groups.nameTooLong', {
      max: props.nameMaxLength,
      half: Math.floor(props.nameMaxLength / 2)
    })
  }
  if (isDuplicateGroupName(trimmed, excludeId)) {
    return t('app.tunnels.groups.nameDuplicate')
  }
  return ''
}

function startRename(group) {
  editingGroupId.value = group.id
  editingGroupName.value = group.name
  localValidationError.value = ''
}

function cancelRename() {
  editingGroupId.value = null
  editingGroupName.value = ''
  localValidationError.value = ''
}

function submitRename(group) {
  const name = editingGroupName.value.trim()
  const validationError = validateGroupName(name, group.id)
  if (validationError) {
    localValidationError.value = validationError
    return
  }
  if (name === group.name) {
    cancelRename()
    return
  }
  emit('rename-group', { id: group.id, name })
  cancelRename()
}

function submitCreate() {
  const name = newGroupName.value.trim()
  const validationError = validateGroupName(name)
  if (validationError) {
    localValidationError.value = validationError
    return
  }
  emit('create-group', name)
  newGroupName.value = ''
  localValidationError.value = ''
}

function canDrag(group) {
  return editingGroupId.value == null || Number(editingGroupId.value) !== Number(group?.id)
}

function onDragStart(event, group) {
  if (!canDrag(group) || event.target.closest('.tunnel-group-item-actions')) {
    event.preventDefault()
    return
  }
  draggingGroupId.value = Number(group.id)
  dragOverGroupId.value = null
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', String(group.id))
}

function onDragOver(event, group) {
  if (draggingGroupId.value == null) return
  if (Number(group.id) === Number(draggingGroupId.value)) return
  event.preventDefault()
  event.dataTransfer.dropEffect = 'move'
  dragOverGroupId.value = Number(group.id)
}

function onDragLeave(group) {
  if (Number(dragOverGroupId.value) === Number(group.id)) {
    dragOverGroupId.value = null
  }
}

function onDrop(event, targetGroup) {
  event.preventDefault()
  const fromId = Number(draggingGroupId.value)
  const toId = Number(targetGroup.id)
  draggingGroupId.value = null
  dragOverGroupId.value = null
  if (!Number.isInteger(fromId) || fromId <= 0 || fromId === toId) return

  const list = [...orderedGroups.value]
  const fromIndex = list.findIndex((item) => Number(item.id) === fromId)
  const toIndex = list.findIndex((item) => Number(item.id) === toId)
  if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) return

  const [moved] = list.splice(fromIndex, 1)
  list.splice(toIndex, 0, moved)
  orderedGroups.value = list
  emit(
    'reorder-groups',
    list.map((item) => Number(item.id))
  )
}

function onDragEnd() {
  draggingGroupId.value = null
  dragOverGroupId.value = null
}
</script>

<template>
  <div v-if="show" class="overlay">
    <div class="dialog-card compact-dialog tunnel-group-dialog">
      <div class="dialog-head">
        <h3 class="dialog-title">{{ $t('app.tunnels.groups.manageTitle') }}</h3>
      </div>
      <form
        class="dialog-body"
        autocapitalize="none"
        autocorrect="off"
        spellcheck="false"
        @submit.prevent="submitCreate"
      >
        <div class="tunnel-group-create-row">
          <input
            id="tunnel-group-new-name"
            v-model="newGroupName"
            class="form-control"
            type="text"
            :maxlength="nameMaxLength"
            autocapitalize="none"
            autocorrect="off"
            spellcheck="false"
            :placeholder="$t('app.tunnels.groups.newGroupPlaceholder')"
            @keydown.enter.prevent="submitCreate"
          />
          <button type="submit" class="btn btn-primary tunnel-group-create-btn" :disabled="!newGroupName.trim()">
            {{ $t('app.tunnels.groups.create') }}
          </button>
        </div>

        <div v-if="orderedGroups.length === 0" class="tunnel-group-empty text-muted">
          {{ $t('app.tunnels.groups.empty') }}
        </div>

        <ul v-else class="list-group tunnel-group-list">
          <li
            v-for="group in orderedGroups"
            :key="group.id"
            class="list-group-item tunnel-group-list-item"
            :class="{
              'is-draggable': canDrag(group),
              'is-dragging': Number(draggingGroupId) === Number(group.id),
              'is-drag-over': Number(dragOverGroupId) === Number(group.id)
            }"
            :draggable="canDrag(group)"
            @dragstart="onDragStart($event, group)"
            @dragover="onDragOver($event, group)"
            @dragleave="onDragLeave(group)"
            @drop="onDrop($event, group)"
            @dragend="onDragEnd"
          >
            <template v-if="editingGroupId === group.id">
              <div class="tunnel-group-edit-row">
                <input
                  v-model="editingGroupName"
                  class="form-control"
                  type="text"
                  :maxlength="nameMaxLength"
                  autocapitalize="none"
                  autocorrect="off"
                  spellcheck="false"
                  @keydown.enter.prevent="submitRename(group)"
                  @keydown.esc.prevent="cancelRename"
                />
                <div class="tunnel-group-edit-actions">
                  <button type="button" class="btn btn-primary" @click="submitRename(group)">
                    {{ $t('app.common.save') }}
                  </button>
                  <button type="button" class="btn btn-outline-secondary" @click="cancelRename">
                    {{ $t('app.common.cancel') }}
                  </button>
                </div>
              </div>
            </template>
            <template v-else>
              <div class="tunnel-group-item-row">
                <span class="tunnel-group-item-name text-truncate">{{ group.name }}</span>
                <div class="tunnel-group-item-actions">
                  <div class="btn-group btn-group-sm action-btn-group" role="group" :aria-label="$t('app.tunnels.groups.manageTitle')">
                    <IconActionButton
                      button-class="btn-outline-secondary"
                      :title="$t('app.tunnels.groups.rename')"
                      :aria-label="$t('app.tunnels.groups.rename')"
                      icon-class="bi-sliders"
                      @click="startRename(group)"
                    />
                    <IconActionButton
                      button-class="btn-outline-danger"
                      :title="$t('app.tunnels.groups.delete')"
                      :aria-label="$t('app.tunnels.groups.delete')"
                      icon-class="bi-trash3"
                      @click="emit('delete-group', group)"
                    />
                  </div>
                </div>
              </div>
            </template>
          </li>
        </ul>

        <div class="form-check form-switch tunnel-group-pref-switch mt-3 mb-0">
          <input
            id="hideEmptyUngroupedSwitch"
            class="form-check-input"
            type="checkbox"
            :checked="hideEmptyUngrouped"
            @change="emit('update:hide-empty-ungrouped', $event.target.checked)"
          />
          <label class="form-check-label" for="hideEmptyUngroupedSwitch">
            {{ $t('app.tunnels.groups.hideEmptyUngrouped') }}
          </label>
        </div>

        <p v-if="displayError" class="text-danger tunnel-group-error mb-0 mt-2">{{ displayError }}</p>
      </form>
      <div class="dialog-footer">
        <button type="button" class="btn btn-outline-secondary" @click="emit('close')">
          {{ $t('app.common.close') }}
        </button>
      </div>
    </div>
  </div>
</template>
