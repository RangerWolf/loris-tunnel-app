<script setup>
import CloudSyncCard from '../common/CloudSyncCard.vue'

defineEmits(['confirm-action', 'reload-state'])
</script>

<template>
  <section class="page-fade visual-page sync-page">
    <CloudSyncCard
      @confirm-action="$emit('confirm-action', $event)"
      @reload-state="$emit('reload-state')"
    />
  </section>
</template>
