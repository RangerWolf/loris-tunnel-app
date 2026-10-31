<script setup>
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import CardHeader from './CardHeader.vue'
defineProps({ feature: { type: String, required: true }, description: { type: String, required: true } })
const emit = defineEmits(['close', 'upgrade'])
const { t } = useI18n()
const dialog = ref(null)
onMounted(() => dialog.value.showModal())
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="upgrade-prompt visual-page lt-card" aria-labelledby="upgrade-title" aria-describedby="upgrade-description" @cancel.prevent="emit('close')" @click="$event.target === $event.currentTarget && emit('close')">
      <div>
        <CardHeader :title="feature" icon="bi-stars" />
        <div class="lt-card-body">
          <h3 id="upgrade-title" class="fs-6">{{ t('config.proFeature', { feature }) }}</h3>
          <p id="upgrade-description" class="config-desc">{{ description }}</p>
          <div class="d-flex justify-content-end gap-2 mt-3">
            <button class="btn btn-sm btn-outline-secondary" @click="emit('close')">{{ t('config.notNow') }}</button>
            <button class="btn btn-sm btn-primary" @click="emit('upgrade')">{{ t('config.upgradePro') }}</button>
          </div>
        </div>
      </div>
    </dialog>
  </Teleport>
</template>

<style scoped>
.upgrade-prompt { padding: 0; width: min(440px, calc(100vw - 32px)); max-height: calc(100vh - 32px); }
.upgrade-prompt::backdrop { background: var(--lt-overlay-bg); }
</style>
