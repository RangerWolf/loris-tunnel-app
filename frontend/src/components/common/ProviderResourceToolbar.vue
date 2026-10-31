<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  title: { type: String, default: '' },
  totalCount: { default: null },
  visibleCount: { type: Number, default: 0 },
  searchQuery: { type: String, default: '' },
  searchPlaceholder: { type: String, default: '' },
})

const emit = defineEmits(['update-search-query'])
const { t } = useI18n()

const countLabel = computed(() => (
  props.totalCount === null || props.totalCount === undefined ? '—' : String(props.totalCount)
))
const showMatch = computed(() => (
  String(props.searchQuery || '').trim() !== ''
  && props.totalCount !== null
  && props.totalCount !== undefined
))

function onInput(event) {
  emit('update-search-query', event.target.value)
}
</script>

<template>
  <div class="provider-toolbar">
    <div class="provider-toolbar-heading">
      <h2 class="provider-resource-title">{{ title }}</h2>
      <span class="provider-resource-count">· {{ countLabel }}</span>
      <span v-if="showMatch" class="provider-resource-match">
        {{ t('app.providers.resources.matched', { matched: visibleCount, total: totalCount }) }}
      </span>
    </div>
    <div class="provider-toolbar-controls">
      <input
        :value="searchQuery"
        type="search"
        class="provider-search"
        :placeholder="searchPlaceholder"
        :aria-label="searchPlaceholder"
        autocomplete="off"
        autocapitalize="none"
        autocorrect="off"
        spellcheck="false"
        @input="onInput"
      />
      <div class="provider-toolbar-actions">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>
