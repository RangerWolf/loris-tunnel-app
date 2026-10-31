<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({ tunnelForm: { type: Object, required: true }, hint: { type: String, default: '' } })
const { t } = useI18n()
const address = ref('')
const input = ref(null)
const error = ref('')
let updating = false
let touched = false
watch(() => [props.tunnelForm.localHost, props.tunnelForm.localPort, props.tunnelForm.mode, props.tunnelForm.provider], ([host, port, mode]) => {
  if (updating) return
  const hostname = String(host || '')
  const formattedHost = hostname.includes(':') ? `[${hostname}]` : hostname
  address.value = `${props.tunnelForm.provider === 'frp' && mode === 'tcp' ? 'tcp' : mode === 'https' ? 'https' : 'http'}://${formattedHost}:${port || 3000}`
  error.value = ''
  input.value?.setCustomValidity('')
}, { immediate: true, flush: 'sync' })

function invalidMessage() {
  return t(props.tunnelForm.provider === 'frp' ? 'app.frp.localAddressInvalid' : 'app.modals.tunnel.localServiceInvalid')
}

function updateAddress(event) {
  const value = event.target.value.trim()
  address.value = event.target.value
  updating = true
  try {
    const url = new URL(value)
    const port = Number(url.port || (url.protocol === 'https:' ? 443 : url.protocol === 'http:' ? 80 : 0))
    const protocols = props.tunnelForm.provider === 'frp' ? ['tcp:', 'http:'] : ['http:', 'https:']
    const mismatch = Boolean(url.hostname && !protocols.includes(url.protocol))
    if (!protocols.includes(url.protocol) || !url.hostname || url.username || url.password || url.search || url.hash || !['', '/'].includes(url.pathname) || port < 1 || port > 65535 || (props.tunnelForm.portLocked && port !== Number(props.tunnelForm.localPort))) throw Object.assign(new Error('invalid address'), { mismatch })
    props.tunnelForm.localHost = url.hostname.replace(/^\[|\]$/g, '')
    props.tunnelForm.localPort = port
    if (props.tunnelForm.provider === 'cloudflare') props.tunnelForm.mode = url.protocol.slice(0, -1)
    if (props.tunnelForm.provider === 'frp') props.tunnelForm.mode = url.protocol === 'tcp:' ? 'tcp' : 'http'
    error.value = ''
    event.target.setCustomValidity('')
  } catch (err) {
    props.tunnelForm.localHost = value
    const message = invalidMessage()
    event.target.setCustomValidity(message)
    error.value = touched || err.mismatch ? message : ''
  } finally {
    updating = false
  }
}

function finishAddress(event) {
  touched = true
  error.value = event.target.validationMessage || ''
}
</script>

<template>
  <div class="col-12">
    <label class="form-label" for="tunnel-local-service">{{ $t('app.modals.tunnel.localServiceAddress') }}</label>
    <input id="tunnel-local-service" ref="input" :value="address" class="form-control" :class="{ 'is-invalid': error }" type="url" placeholder="http://127.0.0.1:3000" autocapitalize="none" autocorrect="off" spellcheck="false" required @input="updateAddress" @blur="finishAddress" />
    <div v-if="error" class="field-note is-error">{{ error }}</div>
    <div v-else class="field-note">{{ hint || $t(tunnelForm.portLocked ? 'app.modals.tunnel.localServicePortLocked' : 'app.modals.tunnel.localServiceHint') }}</div>
  </div>
</template>
