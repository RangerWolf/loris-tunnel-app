<script setup>
defineProps({ title: { type: String, required: true }, icon: { type: String, default: '' } })
</script>

<template>
  <div class="lt-card-header">
    <div class="d-flex align-items-center gap-2 flex-wrap">
      <h2 class="lt-card-title"><i v-if="icon" class="bi" :class="icon" aria-hidden="true" />{{ title }}</h2>
      <slot name="status" />
    </div>
    <slot />
  </div>
</template>
