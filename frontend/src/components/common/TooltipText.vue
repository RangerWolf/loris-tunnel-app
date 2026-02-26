<script setup>
import { onMounted, onBeforeUnmount, watch, ref } from 'vue'
import { Tooltip } from 'bootstrap'

const props = defineProps({
  text: {
    type: String,
    required: true
  },
  className: {
    type: String,
    default: 'cell-ellipsis'
  },
  placement: {
    type: String,
    default: 'top'
  }
})

const textRef = ref(null)
let tooltipInstance = null

function setupTooltip() {
  if (!textRef.value) return

  if (tooltipInstance) {
    tooltipInstance.dispose()
    tooltipInstance = null
  }

  tooltipInstance = new Tooltip(textRef.value)
}

onMounted(() => {
  setupTooltip()
})

onBeforeUnmount(() => {
  if (tooltipInstance) {
    tooltipInstance.dispose()
    tooltipInstance = null
  }
})

watch(
  () => props.text,
  (newText) => {
    if (tooltipInstance) {
      tooltipInstance.setContent({ '.tooltip-inner': newText || '' })
    } else {
      setupTooltip()
    }
  }
)
</script>

<template>
  <span
    ref="textRef"
    :class="className"
    data-bs-toggle="tooltip"
    :data-bs-placement="placement"
    :data-bs-title="text"
  >
    {{ text }}
  </span>
</template>
