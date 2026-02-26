<script setup>
defineProps({
  currentPage: {
    type: Object,
    default: null
  },
  activePage: {
    type: String,
    required: true
  }
})

defineEmits(['new-jumper', 'new-tunnel', 'import-tunnel'])
</script>

<template>
  <header class="top-header">
    <div>
      <h1 class="page-title">{{ currentPage?.title }}</h1>
      <p class="page-subtitle">{{ currentPage?.subtitle }}</p>
    </div>
    <div class="d-flex align-items-center gap-2">
      <button v-if="activePage === 'jumpers'" type="button" class="btn btn-primary" @click="$emit('new-jumper')">
        {{ $t('app.header.newJumper') }}
      </button>
      <button v-if="activePage === 'tunnels'" type="button" class="btn btn-outline-primary" @click="$emit('import-tunnel')">
        <i class="bi bi-file-earmark-plus me-1"></i>
        {{ $t('app.header.importTunnel') }}
      </button>
      <button v-if="activePage === 'tunnels'" type="button" class="btn btn-primary" @click="$emit('new-tunnel')">
        {{ $t('app.header.newTunnel') }}
      </button>
    </div>
  </header>
</template>
