<script setup>
import { useI18n } from 'vue-i18n'
import TooltipText from '../common/TooltipText.vue'
import ProviderResourceToolbar from '../common/ProviderResourceToolbar.vue'
import ProviderRowActions from '../common/ProviderRowActions.vue'

const props = defineProps({
  jumpers: { type: Array, required: true },
  totalCount: { default: null },
  tunnels: { type: Array, default: () => [] },
  searchQuery: { type: String, default: '' },
})

const emit = defineEmits([
  'copy-jumper',
  'edit-jumper',
  'delete-jumper',
  'update-search-query',
  'import-jumper',
  'new-jumper',
])

const { t } = useI18n()

function endpoint(jumper) {
  const host = String(jumper?.host || '')
  const port = String(jumper?.port ?? '')
  if (host.includes(':') && !host.startsWith('[')) return `[${host}]:${port}`
  return `${host}:${port}`
}

function keyFileName(path) {
  const value = String(path || '').trim()
  if (!value) return ''
  const parts = value.split(/[/\\]/)
  return parts[parts.length - 1] || value
}

function shortAuth(type) {
  if (type === 'ssh_key') return t('app.providers.resources.auth.sshKey')
  if (type === 'password') return t('app.providers.resources.auth.password')
  if (type === 'ssh_agent') return t('app.providers.resources.auth.sshAgent')
  return t('app.providers.resources.auth.unknown')
}

function authFile(jumper) {
  if (jumper?.authType !== 'ssh_key') return ''
  return keyFileName(jumper.keyPath)
}

function nameTitle(jumper) {
  const name = String(jumper?.name || '')
  const notes = String(jumper?.notes || '').trim()
  return notes ? `${name}\n${notes}` : name
}

function linkedNames(jumper) {
  const id = String(jumper?.id)
  const names = []
  const seen = new Set()
  for (const tunnel of props.tunnels || []) {
    if ((tunnel?.provider || 'ssh') !== 'ssh') continue
    const ids = Array.isArray(tunnel.jumperIds) ? tunnel.jumperIds : []
    if (!ids.some((item) => String(item) === id)) continue
    const key = tunnel.id ?? `${names.length}`
    if (seen.has(key)) continue
    seen.add(key)
    names.push(String(tunnel.name || ''))
  }
  return names
}

function linkText(count) {
  return count ? t('app.providers.resources.linkedCount', { count }) : t('app.providers.resources.unlinked')
}

function menuItems() {
  return [
    { key: 'clone', label: t('app.providers.resourceActions.clone') },
    { key: 'delete', label: t('app.jumpers.actions.delete'), danger: true, separatorBefore: true },
  ]
}

function onAction(key, jumper) {
  if (key === 'clone') emit('copy-jumper', jumper)
  else if (key === 'delete') emit('delete-jumper', jumper)
}

const searching = () => String(props.searchQuery || '').trim() !== ''
</script>

<template>
  <section class="provider-card">
    <ProviderResourceToolbar
      :title="t('app.providers.resources.jumpers')"
      :total-count="totalCount"
      :visible-count="jumpers.length"
      :search-query="searchQuery"
      :search-placeholder="t('app.providers.resources.searchJumpers')"
      @update-search-query="emit('update-search-query', $event)"
    >
      <template #actions>
        <button type="button" class="btn btn-sm btn-outline-secondary provider-tool-btn" @click="emit('import-jumper')">
          {{ t('app.header.importTunnel') }}
        </button>
        <button type="button" class="btn btn-sm btn-primary provider-tool-btn" @click="emit('new-jumper')">
          {{ t('app.header.newJumper') }}
        </button>
      </template>
    </ProviderResourceToolbar>
    <div class="provider-table-wrap">
      <table class="table align-middle mb-0 provider-table provider-ssh-table">
        <colgroup>
          <col class="provider-col-name" />
          <col class="provider-col-address" />
          <col class="provider-col-auth" />
          <col class="provider-col-links" />
          <col class="provider-col-actions" />
        </colgroup>
        <thead>
          <tr>
            <th>{{ t('app.jumpers.table.name') }}</th>
            <th>{{ t('app.providers.resources.connection') }}</th>
            <th>{{ t('app.jumpers.table.auth') }}</th>
            <th>{{ t('app.providers.resources.linkedColumn') }}</th>
            <th class="text-end">{{ t('app.jumpers.table.action') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!jumpers.length">
            <td colspan="5" class="provider-state-cell">
              <template v-if="totalCount === 0">{{ t('app.providers.resources.emptyJumpers') }}</template>
              <template v-else-if="searching()">
                {{ t('app.providers.resources.noMatch') }}
                <button type="button" class="btn btn-link btn-sm provider-clear-search" @click="emit('update-search-query', '')">
                  {{ t('app.providers.resources.clearSearch') }}
                </button>
              </template>
              <template v-else>—</template>
            </td>
          </tr>
          <tr v-for="jumper in jumpers" :key="jumper.id">
            <td>
              <TooltipText :text="jumper.name" :title="nameTitle(jumper)" class-name="cell-ellipsis provider-name" />
            </td>
            <td>
              <TooltipText :text="endpoint(jumper)" class-name="cell-ellipsis provider-main" />
              <TooltipText :text="t('app.providers.resources.user', { user: jumper.user })" class-name="cell-ellipsis provider-sub" />
            </td>
            <td>
              <TooltipText :text="shortAuth(jumper.authType)" class-name="cell-ellipsis provider-main" />
              <TooltipText
                v-if="authFile(jumper)"
                :text="authFile(jumper)"
                :title="jumper.keyPath"
                class-name="cell-ellipsis provider-sub"
              />
            </td>
            <td>
              <TooltipText
                :text="linkText(linkedNames(jumper).length)"
                :title="linkedNames(jumper).join(', ') || linkText(0)"
                :class-name="linkedNames(jumper).length ? 'cell-ellipsis' : 'cell-ellipsis provider-muted'"
              />
            </td>
            <td class="text-end provider-actions-cell">
              <ProviderRowActions
                :primary="{ label: t('app.jumpers.actions.edit'), icon: 'bi-pencil', disabled: false }"
                :items="menuItems()"
                :menu-label="t('app.providers.resourceActions.more')"
                @primary="emit('edit-jumper', jumper)"
                @select="onAction($event, jumper)"
              />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
