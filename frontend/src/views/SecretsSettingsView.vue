<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onDeactivated, ref, watch } from 'vue'
import { Button } from '@madsholme/ui/button'
import { Input } from '@madsholme/ui/input'
import { state } from '../dashboard'
import { safeURL } from '../format'
import { secretRequest, transientSecret, validRetention, type BackupConfig, type CleanupConfig, type SecretReply, type SecretRequest } from '../secrets'
const management = ref<SecretReply['management']>({}), integrations = ref<NonNullable<SecretReply['management']>['integrations']>([])
const cleanup = ref<CleanupConfig>({ automatic: true, branch: '7d', worktree: '7d', project: '30d' })
const backup = ref<BackupConfig>({ recipient: '', directory: '', daily: true, retain: 30 })
const retired = ref<NonNullable<SecretReply['resources']>>([])
const agents = ref<Record<string,string>>({})
const image = ref(''), service = ref('cloudflare'), key = ref('token'), password = ref('')
const error = ref(''), message = ref(''), busy = ref(false), pending = ref<{ request: SecretRequest; reply: SecretReply }>()
const { value, clear, reveal } = transientSecret()
let generation = 0
const valid = computed(() => [cleanup.value.branch, cleanup.value.worktree, cleanup.value.project].every(validRetention))
function reset() { generation++; clear(); password.value = ''; pending.value = undefined; management.value = {}; integrations.value = []; retired.value = [] }
async function load() {
  reset(); if (!state.session.signed_in) return
  const current = generation; error.value = ''; busy.value = true
  try {
    const [status, policy, entries, resources, skills] = await Promise.all([
      secretRequest({ operation: 'vault-status' }), secretRequest({ operation: 'cleanup-config' }),
      secretRequest({ operation: 'integration-list' }), secretRequest({ operation: 'cleanup-status' }), secretRequest({ operation: 'agents-status' }),
    ])
    if (current !== generation) return
    agents.value = skills.management?.agents || {}
    management.value = status.management
    if (status.management?.config?.backup) backup.value = { ...backup.value, ...status.management.config.backup }
    if (policy.cleanup) cleanup.value = policy.cleanup
    integrations.value = entries.management?.integrations || []; retired.value = resources.resources || []
  } catch (e) { if (current === generation) error.value = e instanceof Error ? e.message : String(e) }
  finally { if (current === generation) busy.value = false }
}
async function action(request: SecretRequest) {
  const current = generation; busy.value = true; error.value = ''; clear()
  try {
    const reply = await secretRequest(request)
    if (current !== generation) return
    if (reply.confirmation && !request.confirm) { pending.value = { request, reply }; return }
    message.value = reply.message || 'Saved'; await load()
  } catch (e) { if (current === generation) error.value = e instanceof Error ? e.message : String(e) }
  finally { busy.value = false }
}
async function revealIntegration(service: string,key: string) {
  error.value=''
  try {await reveal(async () => (await secretRequest({ operation:'integration-get',service,key,reveal:true })).value || '')}
  catch(e){error.value=e instanceof Error ? e.message : String(e)}
}
async function saveIntegration() {
  const value = password.value; password.value = ''
  await action({ operation: 'integration-set', service: service.value, key: key.value, value })
}
watch(() => state.session.signed_in, load, { immediate: true })
onActivated(load); onDeactivated(reset); onBeforeUnmount(reset)
</script>
<template>
  <section class="secrets-settings">
    <header><h1>Secrets settings</h1><Button variant="outline" :disabled="busy" @click="load">Refresh</Button></header>
    <p v-if="!state.session.signed_in">Sign in to manage secrets.</p>
    <template v-else>
      <p v-if="error" role="alert">{{ error }}</p><p v-if="message" role="status">{{ message }}</p>
      <section class="workspace-surface"><h2>OpenBao</h2><p>Status: {{ management?.status?.state || 'Loading' }} · {{ management?.status?.version || management?.config?.image }}</p>
        <a v-if="safeURL(management?.url)" :href="safeURL(management?.url)" target="_blank" rel="noopener">Open vault web UI</a>
        <p>Initialize, unseal and provision in your host terminal with <code>devkit vault init</code>, <code>devkit vault unseal</code> and <code>devkit vault provision</code>. Recovery keys stay with you.</p>
        <Button variant="outline" :disabled="busy" @click="action({ operation: 'vault-setup' })">Start OpenBao</Button>
        <form @submit.prevent="action({ operation: 'vault-update', image })"><label>Update image <Input v-model="image" placeholder="openbao/openbao:2.7.2" /></label><Button :disabled="busy || !image">Preview update</Button></form>
        <p>Updates require an encrypted backup and preserve the bind-mounted data directory. Manual unseal may be required.</p>
      </section>
      <section class="workspace-surface"><h2>Cleanup</h2>
        <form aria-label="Cleanup settings" @submit.prevent="action({ operation: 'cleanup-config', cleanup: { ...cleanup } })">
          <label class="check"><input v-model="cleanup.automatic" type="checkbox" name="automatic" /> Enable automatic cleanup</label>
          <label>Branch retention <Input v-model="cleanup.branch" name="branch-retention" placeholder="7d or never" /></label>
          <label>Worktree retention <Input v-model="cleanup.worktree" name="worktree-retention" placeholder="7d or never" /></label>
          <label>Project retention <Input v-model="cleanup.project" name="project-retention" placeholder="30d or never" /></label>
          <p>Use a positive duration (for example 7d or 48h), or <code>never</code>. Turning automation off prevents automatic deletion. Explicit deletion and recovery remain available. Existing deadlines are preserved.</p>
          <Button :disabled="busy || !valid">Preview cleanup settings</Button>
        </form>
        <Button v-if="cleanup.resume_after && !cleanup.resume_after.startsWith('0001-')" variant="outline" :disabled="busy" @click="action({ operation: 'cleanup-resume' })">Review overdue cleanup before resuming</Button>
        <ul><li v-for="item in retired" :key="item.id">{{ item.kind }} · {{ item.name }} · {{ item.state }} · {{ item.deadline || 'No expiry' }}
          <Button variant="outline" :disabled="busy" @click="action({ operation: 'cleanup-restore', resource: item.id })">Preview restore</Button>
          <Button variant="outline" :disabled="busy" @click="action({ operation: 'cleanup-purge', resource: item.id })">Preview permanent deletion</Button>
        </li></ul>
      </section>
      <section class="workspace-surface"><h2>Encrypted backups</h2>
        <form @submit.prevent="action({ operation: 'vault-backup-config', backup: { ...backup, retain: Number(backup.retain) } })">
          <label>age recipient <Input v-model="backup.recipient" placeholder="age1…" /></label><label>Backup directory <Input v-model="backup.directory" placeholder="/mnt/backup/devkit" /></label>
          <label>Copies to retain <Input v-model="backup.retain" type="number" min="1" max="3650" /></label><label class="check"><input v-model="backup.daily" type="checkbox" /> Daily backup</label>
          <Button :disabled="busy || !backup.recipient || !backup.directory">Save backup settings</Button>
        </form><p>Choose a mounted off-host destination and keep the decryption key separately. Backups may retain deleted values. Capturing certificate state briefly stops Traefik.</p>
        <Button variant="outline" :disabled="busy" @click="action({ operation: 'vault-backup' })">Create encrypted backup</Button><p>{{ management?.maintenance }}</p>
      </section>
      <section class="workspace-surface"><h2>Agent skills</h2><p v-for="(status, agent) in agents" :key="agent">{{ agent }} · {{ status }}</p><Button variant="outline" :disabled="busy" @click="action({ operation: 'agents-repair' })">Install or repair skills</Button><p>Edited skills are preserved. To add guidance to an existing project, run <code>devkit agents skills install --project /path/to/checkout</code>.</p></section>
      <section class="workspace-surface"><h2>Integrations</h2><ul><li v-for="entry in integrations" :key="`${entry.service}/${entry.key}`">{{ entry.service }} / {{ entry.key }} · {{ entry.source }} <Button v-if="entry.source === 'OpenBao' && entry.service !== 'dns'" variant="outline" @click="revealIntegration(entry.service, entry.key)">Reveal</Button></li></ul>
        <form @submit.prevent="saveIntegration"><label>Service <select v-model="service"><option>cloudflare</option><option>github</option><option>tailscale</option></select></label><label>Key <Input v-model="key" /></label><label>Token <Input v-model="password" type="password" autocomplete="new-password" /></label><Button :disabled="busy || !password || !key">Save credential</Button></form>
        <p>Tailscale API credentials are optional; normal connectivity uses your existing Tailscale login. Git HTTPS binding is configured per repository through the CLI. SSH keys and agent stay unchanged.</p>
      </section>
      <div v-if="pending" class="workspace-surface" role="dialog" aria-label="Review secrets settings"><h2>Review change</h2><p>{{ pending.reply.message }}</p><pre v-if="pending.reply.cleanup">{{ JSON.stringify(pending.reply.cleanup, null, 2) }}</pre><ul><li v-for="item in pending.reply.resources || []" :key="item.id">{{ item.name }} · {{ item.deadline || 'No expiry' }}</li><li v-for="record in pending.reply.records || []" :key="`${record.resource}/${record.name}`">{{ record.name }} · {{ record.scope }}</li></ul><Button :disabled="busy" @click="action({ ...pending.request, confirm: pending.reply.confirmation })">Confirm change</Button> <Button variant="outline" @click="pending = undefined">Cancel</Button></div>
      <div v-if="value" role="status"><output>{{ value }}</output> <Button variant="outline" @click="clear">Hide</Button><p>Hidden after 30 seconds.</p></div>
    </template>
  </section>
</template>
<style scoped>
.secrets-settings{display:grid;gap:1rem}.secrets-settings header{display:flex;align-items:center;justify-content:space-between}.workspace-surface{display:grid;gap:.8rem}form{display:flex;gap:.75rem;align-items:end;flex-wrap:wrap}label{display:grid;gap:.3rem}label.check{display:flex;align-items:center}form p{flex-basis:100%}p,h1,h2{margin:0}select{background:var(--background);color:inherit;border:1px solid var(--border);padding:.5rem;border-radius:.4rem}pre{white-space:pre-wrap}li{margin:.6rem 0}
</style>
