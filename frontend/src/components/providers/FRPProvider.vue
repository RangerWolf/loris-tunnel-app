<script setup>
import CLIUpdateDialog from '../common/CLIUpdateDialog.vue'
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DeleteFRPServer, SaveFRPServer } from '../../../wailsjs/go/main/App'
import { frpState, refreshFRP } from '../../utils/frp'
import ProviderResourceToolbar from '../common/ProviderResourceToolbar.vue'
import ProviderRowActions from '../common/ProviderRowActions.vue'
import TooltipText from '../common/TooltipText.vue'
import IconActionButton from '../common/IconActionButton.vue'

const props = defineProps({ tunnels: { type: Array, default: () => [] } })
const emit = defineEmits(['create-tunnel', 'view-tunnels'])
const { t } = useI18n()
const cliMaintenanceOpen = ref(false)
const search = ref(''), error = ref(''), busy = ref(''), form = ref(null), deleting = ref(null), formError = ref(''), nameInput = ref(null), showToken = ref(false)
let returnFocus
const visible = computed(() => frpState.servers.filter(s => `${s.name} ${s.host}`.toLowerCase().includes(search.value.trim().toLowerCase())))
const linked = server => props.tunnels.filter(tunnel => tunnel.provider === 'frp' && tunnel.frp?.serverId === server.id)
const cliStatusText = computed(() => {
  if (busy.value === 'install') return t('app.frp.installing')
  if (frpState.loading) return t('app.providers.checking')
  return t(frpState.installed ? 'app.providers.installed' : 'app.providers.notInstalled')
})
const cliValue = computed(() => (busy.value === 'install' || frpState.loading || !frpState.installed ? '' : frpState.version))
const cliNote = computed(() => {
  if (busy.value === 'install') return t('app.providers.installPending')
  if (frpState.loading) return ''
  if (!frpState.installed && frpState.detail && frpState.detail !== 'frpc is not installed') return frpState.detail
  if (!frpState.installed) return t('app.frp.description')
  return ''
})
function linkText(count) {
  return count ? t('app.providers.resources.linkedCount', { count }) : t('app.providers.resources.unlinked')
}
async function run(action, fn) {
  if (busy.value) return
  busy.value = action; error.value = ''; formError.value = ''
  try { await fn(); await refreshFRP() } catch (err) { (form.value || deleting.value ? formError : error).value = String(err) }
  finally { busy.value = '' }
}
async function open(server) {
  returnFocus = document.activeElement; showToken.value = false; formError.value = ''
  form.value = server ? { ...server, token: '', clearToken: false } : { name: '', host: '', port: 7000, token: '', user: '', publicHost: '', httpPort: 80, tls: true }
  await nextTick(); nameInput.value?.focus()
}
function close() { if (busy.value) return; form.value = null; deleting.value = null; showToken.value = false; formError.value = ''; nextTick(() => returnFocus?.focus()) }
async function save() { await run('save', async () => { await SaveFRPServer({ ...form.value }); form.value = null; showToken.value = false; returnFocus?.focus() }) }
function action(key, server) {
  if (key === 'create') emit('create-tunnel', server)
  if (key === 'edit') open(server)
  if (key === 'view') emit('view-tunnels', server)
  if (key === 'delete') { returnFocus = document.activeElement; formError.value = ''; deleting.value = server }
}
function trap(event) {
  if (event.key === 'Escape') { event.preventDefault(); close() }
  if (event.key !== 'Tab') return
  const items = [...event.currentTarget.querySelectorAll('button:not([disabled]),input:not([disabled]),summary')].filter(el => el.getClientRects().length)
  const first = items[0], last = items.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
onMounted(() => run('check', refreshFRP))
</script>

<template>
  <CLIUpdateDialog v-if="cliMaintenanceOpen" provider="frp" @close="cliMaintenanceOpen = false" @changed="run('check', refreshFRP)" />
  <div class="provider-stack">
    <section class="provider-summary" :aria-busy="frpState.loading || busy === 'install'">
      <div class="provider-summary-row">
        <span class="provider-summary-label">CLI</span>
        <div class="provider-summary-main">
          <div class="provider-summary-line">
            <span class="provider-summary-status" :class="{ 'is-busy': frpState.loading || busy === 'install' }">
              <i v-if="frpState.loading || busy === 'install'" class="bi bi-arrow-repeat devtunnel-spin" aria-hidden="true" />
              {{ cliStatusText }}
            </span>
            <span v-if="cliValue" class="provider-summary-value" :title="cliValue">· {{ cliValue }}</span>
          </div>
          <p v-if="cliNote" class="provider-summary-note">{{ cliNote }}</p>
          <p class="provider-summary-note">{{ t('app.frp.localOnly') }}</p>
        </div>
        <div class="provider-summary-actions">
          <button type="button" class="btn btn-sm btn-outline-primary provider-tool-btn" :disabled="frpState.loading && !frpState.installed || busy === 'install'" @click="cliMaintenanceOpen = true">{{ t(frpState.loading && !frpState.installed ? 'app.providers.checking' : frpState.installed ? 'app.cliMaintenance.check' : 'app.providers.install') }}</button>
        </div>
      </div>
      <p v-if="error" class="field-warning pb-2 mb-0" role="alert">{{ error }}</p>
    </section>
    <section class="provider-card provider-resource-card">
      <ProviderResourceToolbar :title="t('app.frp.servers')" :total-count="frpState.servers.length" :visible-count="visible.length" :search-query="search" :search-placeholder="t('app.frp.search')" @update-search-query="search = $event">
        <template #actions>
          <button type="button" class="btn btn-sm btn-primary provider-tool-btn" :disabled="!!busy" @click="open(null)">+ {{ t('app.frp.addServer') }}</button>
        </template>
      </ProviderResourceToolbar>
      <div class="provider-table-wrap" :class="{ 'provider-table-empty': !visible.length }">
        <table class="table align-middle mb-0 provider-table provider-frp-table">
          <colgroup>
            <col class="provider-col-name" />
            <col class="provider-col-address" />
            <col class="provider-col-links" />
            <col class="provider-col-actions" />
          </colgroup>
          <thead>
            <tr>
              <th>{{ t('app.frp.name') }}</th>
              <th>{{ t('app.frp.serverAddress') }}</th>
              <th>{{ t('app.providers.resources.linkedColumn') }}</th>
              <th class="text-end">{{ t('app.tunnels.table.action') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="frpState.loading && !frpState.servers.length">
              <td colspan="4" class="provider-state-cell" role="status">
                <span class="spinner-border spinner-border-sm" aria-hidden="true" />
                {{ t('app.providers.checking') }}
              </td>
            </tr>
            <tr v-else-if="!frpState.servers.length">
              <td colspan="4" class="provider-state-cell">{{ t('app.frp.empty') }}</td>
            </tr>
            <tr v-else-if="!visible.length">
              <td colspan="4" class="provider-state-cell">
                {{ t('app.providers.resources.noMatch') }}
                <button type="button" class="btn btn-link btn-sm provider-clear-search" @click="search = ''">{{ t('app.providers.resources.clearSearch') }}</button>
              </td>
            </tr>
            <tr v-for="server in visible" :key="server.id">
              <td><TooltipText :text="server.name" class-name="cell-ellipsis provider-name" /></td>
              <td><TooltipText :text="`${server.host}:${server.port}`" class-name="cell-ellipsis" /></td>
              <td>
                <TooltipText
                  :text="linkText(linked(server).length)"
                  :title="linked(server).map(tunnel => tunnel.name).filter(Boolean).join(', ') || linkText(0)"
                  :class-name="linked(server).length ? 'cell-ellipsis' : 'cell-ellipsis provider-muted'"
                />
              </td>
              <td class="text-end provider-actions-cell"><ProviderRowActions :menu-label="t('app.providers.resourceActions.more')" :items="[{key:'create',label:t('app.frp.createTunnel'),disabled:!!busy},{key:'view',label:t('app.frp.viewTunnels')},{key:'edit',label:t('app.frp.edit'),disabled:!!busy},{key:'delete',label:t('app.frp.delete'),danger:true,separatorBefore:true,disabled:!!busy || linked(server).length > 0}]" @select="action($event, server)" /></td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
  <Teleport to="body">
    <div v-if="form || deleting" class="devtunnel-resource-overlay" @keydown="trap">
      <div class="dialog-card compact-dialog devtunnel-resource-dialog" role="dialog" aria-modal="true" aria-labelledby="frp-server-title">
        <div class="dialog-head"><h3 id="frp-server-title" class="dialog-title">{{ t(deleting ? 'app.frp.deleteServer' : form.id ? 'app.frp.editServer' : 'app.frp.addServer') }}</h3></div>
        <form v-if="form" id="frp-server-form" class="dialog-body" @submit.prevent="save">
          <fieldset :disabled="!!busy" class="border-0 p-0 m-0"><div class="row g-3">
            <div class="col-12"><label for="frp-name" class="form-label">{{ t('app.frp.name') }}</label><input id="frp-name" ref="nameInput" v-model="form.name" class="form-control" maxlength="100" autocapitalize="none" autocorrect="off" spellcheck="false" required /></div>
            <div class="col-8"><label for="frp-host" class="form-label">{{ t('app.frp.serverAddress') }}</label><input id="frp-host" v-model.trim="form.host" class="form-control" placeholder="frp.example.com" autocapitalize="none" autocorrect="off" spellcheck="false" required /></div>
            <div class="col-4"><label for="frp-port" class="form-label">{{ t('app.frp.serverPort') }}</label><input id="frp-port" v-model.number="form.port" class="form-control" type="number" min="1" max="65535" required /></div>
            <div class="col-12"><label for="frp-token" class="form-label">Token</label><div class="d-flex gap-2"><input id="frp-token" v-model="form.token" class="form-control" :type="showToken ? 'text' : 'password'" autocomplete="new-password" autocapitalize="none" autocorrect="off" spellcheck="false" :placeholder="form.hasToken ? t('app.frp.keepToken') : t('app.frp.optionalToken')" /><IconActionButton :icon-class="showToken ? 'bi-eye-slash' : 'bi-eye'" :title="t(showToken ? 'secret.hide' : 'secret.show')" :aria-label="t(showToken ? 'secret.hide' : 'secret.show')" @click="showToken = !showToken" /></div></div>
            <div v-if="form.hasToken" class="col-12"><label><input v-model="form.clearToken" type="checkbox" /> {{ t('app.frp.clearToken') }}</label></div>
            <div class="col-12"><details><summary>{{ t('app.frp.advanced') }}</summary><div class="row g-3 mt-0">
              <div class="col-12"><label for="frp-user" class="form-label">{{ t('app.frp.namespace') }}</label><input id="frp-user" v-model.trim="form.user" class="form-control" autocapitalize="none" autocorrect="off" spellcheck="false" /></div>
              <div class="col-8"><label for="frp-public-host" class="form-label">{{ t('app.frp.publicHost') }}</label><input id="frp-public-host" v-model.trim="form.publicHost" :placeholder="form.host" class="form-control" autocapitalize="none" autocorrect="off" spellcheck="false" /></div>
              <div class="col-4"><label for="frp-http-port" class="form-label">{{ t('app.frp.httpPort') }}</label><input id="frp-http-port" v-model.number="form.httpPort" class="form-control" type="number" min="1" max="65535" required /></div>
              <div class="col-12"><label><input v-model="form.tls" type="checkbox" /> {{ t('app.frp.tls') }}</label></div>
            </div></details></div>
          </div></fieldset>
        </form>
        <div v-else class="dialog-body"><p>{{ t('app.frp.deleteConfirm', { name: deleting.name }) }}</p></div>
        <p v-if="formError" class="field-warning px-3" role="alert">{{ formError }}</p>
        <div class="dialog-footer">
          <button class="btn btn-outline-secondary" :disabled="!!busy" @click="close">{{ t('app.common.cancel') }}</button>
          <button v-if="form" class="btn btn-primary" type="submit" form="frp-server-form" :disabled="!!busy">{{ t('app.common.save') }}</button>
          <button v-else class="btn btn-danger" :disabled="!!busy" @click="run('delete', async () => { await DeleteFRPServer(deleting.id); deleting = null; returnFocus?.focus() })">{{ t('app.frp.delete') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
