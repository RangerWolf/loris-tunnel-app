<script setup>
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  code: { type: String, default: '' },
  error: { type: String, default: '' },
  submitting: { type: Boolean, default: false },
  initialTab: { type: String, default: 'purchase' },
})
const emit = defineEmits(['update:code', 'close', 'submit', 'purchase'])
const { t } = useI18n()
const dialog = ref(null)
const tab = ref(props.initialTab)
const plans = [
  { id: 'monthly', price: '1.90', url: 'https://www.16688.com.cn/goods/G95265345' },
  { id: 'yearly', price: '19.99', url: 'https://www.16688.com.cn/goods/G53380769' },
]
onMounted(() => {
  const el = dialog.value
  el.showModal()
  // showModal 会把焦点交给第一个按钮（关闭）。打开时改放到对话框上，避免关闭按钮呈选中态。
  const placeFocus = () => el.focus({ preventScroll: true })
  placeFocus()
  requestAnimationFrame(placeFocus)
})
function close() {
  if (!props.submitting) emit('close')
}
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" autofocus tabindex="-1" class="pro-purchase visual-page lt-card" aria-labelledby="pro-purchase-title" @cancel.prevent="close" @click="$event.target === $event.currentTarget && close()">
      <div class="dialog-head d-flex align-items-center justify-content-between gap-2">
        <h3 id="pro-purchase-title" class="dialog-title">{{ t('purchase.title') }}</h3>
        <button type="button" class="btn-close pro-close" :aria-label="t('app.common.cancel')" :disabled="submitting" @click="close" />
      </div>
      <div class="dialog-body">
        <div class="pro-switch mb-3" role="group" :aria-label="t('purchase.title')">
          <button type="button" class="btn btn-sm" :class="tab === 'purchase' ? 'btn-primary' : 'btn-outline-secondary'" :aria-pressed="tab === 'purchase'" :disabled="submitting" @click="tab = 'purchase'">{{ t('purchase.buyTab') }}</button>
          <button type="button" class="btn btn-sm" :class="tab === 'activate' ? 'btn-primary' : 'btn-outline-secondary'" :aria-pressed="tab === 'activate'" :disabled="submitting" @click="tab = 'activate'">{{ t('purchase.activateTab') }}</button>
        </div>
        <div v-if="tab === 'purchase'">
          <p class="text-body-secondary small">{{ t('purchase.intro') }}</p>
          <div class="pro-plans">
            <div v-for="plan in plans" :key="plan.id" class="pro-plan">
              <div class="pro-plan-details">
                <div class="pro-plan-heading">
                  <h4 class="pro-plan-title">{{ t(`purchase.${plan.id}`) }}</h4>
                  <span v-if="plan.id === 'yearly'" class="pro-recommended">{{ t('purchase.recommended') }}</span>
                </div>
                <div class="text-body-secondary small mt-1">{{ t(`purchase.${plan.id}Duration`) }}</div>
              </div>
              <div class="pro-price">¥{{ plan.price }} <small class="text-body-secondary">CNY</small></div>
              <button type="button" class="btn btn-sm btn-outline-primary pro-buy" @click="emit('purchase', plan.url)">{{ t(`purchase.buy${plan.id === 'monthly' ? 'Monthly' : 'Yearly'}`) }}</button>
            </div>
          </div>
          <h4 class="fs-6 mt-3">{{ t('purchase.howTo') }}</h4>
          <ol class="small text-body-secondary ps-3 mb-2">
            <li>{{ t('purchase.step1') }}</li>
            <li>{{ t('purchase.step2') }}</li>
            <li>{{ t('purchase.step3') }}</li>
          </ol>
          <p class="small text-body-secondary">{{ t('purchase.deviceHint') }}</p>
          <button type="button" class="btn btn-sm btn-link pro-code-entry p-0" @click="tab = 'activate'">{{ t('purchase.haveCode') }}</button>
        </div>
        <form v-else @submit.prevent="!submitting && emit('submit')">
          <label for="pro-redeem-code" class="form-label">{{ t('config.redeemDialog.codeLabel') }}</label>
          <input id="pro-redeem-code" :value="code" type="text" class="form-control" autocomplete="off" spellcheck="false" :placeholder="t('config.redeemDialog.codePlaceholder')" :disabled="submitting" aria-describedby="pro-code-hint pro-code-error" :aria-invalid="!!error" @input="emit('update:code', $event.target.value)" />
          <div id="pro-code-hint" class="field-note mt-1">{{ t('config.redeemDialog.codeHint') }}</div>
          <p id="pro-code-error" class="form-error mb-0 mt-2" role="alert">{{ error }}</p>
          <div class="d-flex justify-content-end gap-2 mt-3">
            <button type="button" class="btn btn-sm btn-outline-secondary" :disabled="submitting" @click="close">{{ t('app.common.cancel') }}</button>
            <button type="submit" class="btn btn-sm btn-primary" :disabled="submitting">{{ submitting ? t('config.redeemDialog.submitting') : t('purchase.activate') }}</button>
          </div>
        </form>
      </div>
    </dialog>
  </Teleport>
</template>

<style scoped>
.pro-purchase { overflow-y: auto; overflow-wrap: anywhere; padding: 0; width: min(510px, calc(100vw - 32px)); max-height: calc(100vh - 32px); color: var(--lt-ink); background: var(--lt-surface); border: 1px solid var(--lt-border); border-radius: 6px; }
.pro-purchase:focus { outline: none; }
.pro-purchase::backdrop { background: var(--lt-overlay-bg); }
.pro-plans { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; }
.pro-plan { display: flex; flex-direction: column; min-width: 0; padding: 12px; border: 1px solid var(--lt-border); border-radius: 4px; background: var(--lt-surface-soft); }
.pro-switch { display: flex; flex-wrap: wrap; gap: 8px; }
.pro-switch .btn { min-width: 0; white-space: normal; }
.pro-close { flex: 0 0 auto; font-size: 12px; margin: 2px; }
.pro-plan-details { flex: 1; }
.pro-plan-heading { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 6px; }
.pro-plan-title { margin: 0; font-size: 0.875rem; font-weight: 600; }
.pro-recommended { flex: none; padding: 1px 7px; border: 1px solid #ea580c; border-radius: 3px; background: #f97316; color: #fff; font-size: 0.75rem; font-weight: 600; line-height: 1.5; white-space: nowrap; }
.pro-buy { width: 100%; min-height: 32px; white-space: normal; }
.pro-code-entry { font-size: 0.75rem; text-align: start; text-decoration: none; white-space: normal; }
.pro-code-entry:hover { text-decoration: underline; }
.pro-price { white-space: nowrap; margin: 12px 0; font-size: 1.5rem; font-weight: 600; }
.pro-price small { font-size: 0.7rem; font-weight: 400; }
@media (max-width: 400px) { .pro-plans { grid-template-columns: 1fr; } }
</style>
