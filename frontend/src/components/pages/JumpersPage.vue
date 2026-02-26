<script setup>
import IconActionButton from '../common/IconActionButton.vue'
import { ref, watch } from 'vue'

const props = defineProps({
  jumpers: {
    type: Array,
    required: true
  },
  searchQuery: {
    type: String,
    default: ''
  },
  getAuthLabel: {
    type: Function,
    required: true
  }
})

const emit = defineEmits(['copy-jumper', 'edit-jumper', 'delete-jumper', 'update-search-query'])

const localSearchQuery = ref(props.searchQuery)

watch(() => props.searchQuery, (newValue) => {
  localSearchQuery.value = newValue
})

watch(localSearchQuery, (newValue) => {
  emit('update-search-query', newValue)
})
</script>

<template>
  <section class="page-fade panel-card">
    <div class="panel-head">
      <h2 class="panel-title mb-0">{{ $t('app.jumpers.title') }}</h2>
      <div class="search-box">
        <div class="input-group input-group-sm">
          <span class="input-group-text">
            <i class="bi bi-search"></i>
          </span>
          <input
            v-model="localSearchQuery"
            type="text"
            class="form-control"
            :placeholder="$t('app.common.searchPlaceholder')"
            aria-label="Search jumpers"
          />
          <button
            class="btn btn-outline-secondary"
            :class="{ invisible: !localSearchQuery }"
            type="button"
            @click="localSearchQuery = ''"
            :aria-label="$t('app.common.clearSearch')"
            :disabled="!localSearchQuery"
          >
            <i class="bi bi-x"></i>
          </button>
        </div>
      </div>
    </div>
    <div class="table-responsive page-table-wrap">
      <table class="table align-middle mb-0 jumpers-table">
        <thead>
          <tr>
            <th>{{ $t('app.jumpers.table.name') }}</th>
            <th>{{ $t('app.jumpers.table.connection') }}</th>
            <th>{{ $t('app.jumpers.table.auth') }}</th>
            <th>{{ $t('app.jumpers.table.notes') }}</th>
            <th class="text-end jumpers-action-cell">{{ $t('app.jumpers.table.action') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="jumpers.length === 0">
            <td colspan="5" class="text-muted py-4">{{ $t('app.jumpers.noJumpers') }}</td>
          </tr>
          <tr v-for="jumper in jumpers" :key="jumper.id">
            <td class="fw-semibold jumper-name-cell">
              <span class="cell-ellipsis" :title="jumper.name">{{ jumper.name }}</span>
            </td>
            <td class="jumper-conn-cell">
              <div class="cell-ellipsis" :title="jumper.user">{{ jumper.user }}</div>
              <div class="text-muted small cell-ellipsis" :title="`${jumper.host}:${jumper.port}`">
                {{ jumper.host }}:{{ jumper.port }}
              </div>
            </td>
            <td class="jumper-auth-cell">
              <div class="cell-ellipsis" :title="getAuthLabel(jumper.authType)">{{ getAuthLabel(jumper.authType) }}</div>
              <div v-if="jumper.keyPath" class="text-muted small cell-ellipsis" :title="jumper.keyPath">
                {{ jumper.keyPath }}
              </div>
            </td>
            <td class="text-muted jumper-notes-cell">
              <span class="cell-ellipsis" :title="jumper.notes || '-'">{{ jumper.notes || '-' }}</span>
            </td>
            <td class="text-end jumpers-action-cell">
              <div class="btn-group btn-group-sm action-btn-group" role="group" aria-label="Jumper Actions">
                <IconActionButton
                  button-class="btn-outline-primary"
                  :title="$t('app.jumpers.actions.copy')"
                  :aria-label="$t('app.jumpers.actions.copy')"
                  icon-class="bi-copy"
                  @click="$emit('copy-jumper', jumper)"
                />
                <IconActionButton
                  button-class="btn-outline-secondary"
                  :title="$t('app.jumpers.actions.edit')"
                  :aria-label="$t('app.jumpers.actions.edit')"
                  icon-class="bi-sliders"
                  @click="$emit('edit-jumper', jumper)"
                />
                <IconActionButton
                  button-class="btn-outline-danger"
                  :title="$t('app.jumpers.actions.delete')"
                  :aria-label="$t('app.jumpers.actions.delete')"
                  icon-class="bi-trash3"
                  @click="$emit('delete-jumper', jumper)"
                />
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
