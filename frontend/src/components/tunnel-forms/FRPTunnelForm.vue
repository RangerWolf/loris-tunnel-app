<script setup>
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { frpState, refreshFRP, frpAddress, frpDomainName } from '../../utils/frp'
import LocalServiceAddress from './LocalServiceAddress.vue'
import CopyIconButton from '../common/CopyIconButton.vue'
import IconActionButton from '../common/IconActionButton.vue'
defineEmits(['manage'])
const props = defineProps({ tunnelForm: { type: Object, required: true } })
const { t } = useI18n()
const error = ref('')
const server = computed(() => frpState.servers.find(s => s.id === props.tunnelForm.frpServerId))
const preview = computed(() => frpAddress(server.value, props.tunnelForm.mode, props.tunnelForm.frpRemotePort, props.tunnelForm.frpDomain))
onMounted(() => refreshFRP().catch(err => { error.value = String(err) }))
</script>

<template>
  <div class="row g-3">
    <div class="col-12 col-md-6">
      <div class="devtunnel-id-header frp-field-header"><label class="form-label" for="frp-server">{{ t('app.frp.server') }}</label><button type="button" class="btn btn-link btn-sm devtunnel-inline-action" @click="$emit('manage')">{{ t('app.frp.manage') }}</button></div>
      <select id="frp-server" v-model="tunnelForm.frpServerId" class="form-select" required><option value="">{{ t('app.frp.selectServer') }}</option><option v-for="item in frpState.servers" :key="item.id" :value="item.id">{{ item.name }} · {{ item.host }}</option></select>
      <p v-if="!server && tunnelForm.frpServerId" class="field-warning">{{ t('app.frp.missingServer') }}</p>
      <button v-if="!frpState.servers.length" type="button" class="btn btn-link btn-sm px-0" @click="$emit('manage')">{{ t('app.frp.addServer') }}</button>
      <p v-if="error" class="field-warning">{{ error }}</p>
    </div>
    <div v-if="tunnelForm.mode === 'tcp'" class="col-12 col-md-6"><div class="devtunnel-id-header frp-field-header"><label for="frp-remote" class="form-label">{{ t('app.frp.remotePort') }}</label></div><input id="frp-remote" v-model.number="tunnelForm.frpRemotePort" class="form-control" type="number" min="1" max="65535" required /></div>
    <div v-else class="col-12 col-md-6"><div class="devtunnel-id-header frp-field-header justify-content-start gap-1"><label for="frp-domain" class="form-label">{{ t('app.frp.domain') }}</label><IconActionButton button-class="btn-link text-muted frp-domain-help" icon-class="bi-question-circle" :title="t('app.frp.domainHint')" :aria-label="t('app.frp.domainHint')" /></div><input id="frp-domain" v-model.trim="tunnelForm.frpDomain" class="form-control" placeholder="dev.example.com" autocapitalize="none" autocorrect="off" spellcheck="false" required @change="tunnelForm.frpDomain = frpDomainName(tunnelForm.frpDomain)" /></div>
    <div class="col-12"><details :open="!!tunnelForm.frpProxyName"><summary class="field-note">{{ t('app.frpImport.advanced') }}</summary><label class="form-label mt-2" for="frp-proxy">{{ t('app.frpImport.proxyName') }}</label><input id="frp-proxy" v-model="tunnelForm.frpProxyName" type="text" class="form-control" maxlength="64" pattern="[a-zA-Z0-9_-]{1,64}" :placeholder="tunnelForm.name" autocapitalize="none" autocorrect="off" spellcheck="false" /><p class="field-note">{{ t('app.frpImport.proxyHint') }}</p></details></div>
    <LocalServiceAddress :tunnel-form="tunnelForm" :hint="t(tunnelForm.mode === 'tcp' ? 'app.frp.localTCPHint' : 'app.frp.localHTTPHint')" />
    <div class="col-12"><label class="form-label">{{ t('app.frp.preview') }}</label><div class="copy-value-row"><span class="copy-value-text">{{ preview || '—' }}</span><CopyIconButton v-if="preview" :value="preview" @error="error = $event" /></div><div class="field-note">{{ t('app.frp.previewHint') }}</div></div>
    <p v-if="!frpState.installed" class="field-warning">{{ t('app.frp.installFirst') }}</p>
  </div>
</template>

<style scoped>
.frp-field-header {
  align-items: center;
  margin-bottom: 0.5rem;
}

.frp-field-header .form-label {
  margin-bottom: 0;
}

.btn.icon-btn.frp-domain-help {
  min-width: 1rem;
  width: 1rem;
  height: 1rem;
}
</style>
