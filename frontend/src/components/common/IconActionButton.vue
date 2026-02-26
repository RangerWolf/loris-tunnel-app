<script setup>
import { onMounted, onBeforeUnmount, watch, ref } from 'vue'
import { Tooltip } from 'bootstrap'

const props = defineProps({
  buttonClass: {
    type: String,
    default: 'btn-outline-secondary'
  },
  title: {
    type: String,
    required: true
  },
  ariaLabel: {
    type: String,
    required: true
  },
  iconClass: {
    type: String,
    required: true
  },
  disabled: {
    type: Boolean,
    default: false
  }
})

defineEmits(['click'])

const btnRef = ref(null)
let tooltipInstance = null

onMounted(() => {
  if (btnRef.value) {
    tooltipInstance = new Tooltip(btnRef.value, {
      trigger: 'hover'
    })
  }
})

onBeforeUnmount(() => {
  if (tooltipInstance) {
    tooltipInstance.dispose()
    tooltipInstance = null
  }
})

// Update tooltip when title changes (e.g. Start -> Stop)
watch(() => props.title, (newTitle) => {
  if (tooltipInstance) {
    tooltipInstance.setContent({ '.tooltip-inner': newTitle })
  }
})
</script>

<template>
  <button
    ref="btnRef"
    type="button"
    class="btn btn-sm icon-btn"
    :class="buttonClass"
    data-bs-toggle="tooltip"
    data-bs-placement="top"
    :data-bs-title="title"
    :aria-label="ariaLabel"
    :disabled="disabled"
    @click="$emit('click')"
  >
    <i class="bi action-icon" :class="iconClass" />
  </button>
</template>
