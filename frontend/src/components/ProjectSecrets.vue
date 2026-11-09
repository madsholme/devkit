<script setup lang="ts">
import { computed, onBeforeUnmount, onDeactivated, ref, watch } from 'vue'
import { Check, Copy, Eye, EyeOff, UserRound, KeyRound } from 'lucide-vue-next'
import { Button } from '@madsholme/ui/button'
import { Input } from '@madsholme/ui/input'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@madsholme/ui/dialog'
import { state } from '../dashboard'
import { secretRequest, transientSecret, type SecretRecord, type SecretReply, type SecretRequest } from '../secrets'
const props = withDefaults(defineProps<{ root: string; defaultScope?: string; checkouts?: { path: string; branch?: string }[] }>(), { defaultScope: 'project' })
const emit = defineEmits<{ context: [reply: SecretReply] }>()
const checkoutRoot = ref(props.root)
const entryType = ref<'value' | 'login'>('value'), username = ref(''), mapEnvironment = ref(false), usernameEnv = ref(''), passwordEnv = ref('')
const loginFields = ['username', 'password'] as const
const scope = ref(props.defaultScope), branch = ref(''), key = ref(''), password = ref('')
const records = ref<SecretRecord[]>([]), context = ref<SecretReply['context']>(), resources = ref<NonNullable<SecretReply['resources']>>([])
const busy = ref(false), error = ref(''), message = ref(''), history = ref<SecretReply['history']>()
const historyName = ref(''), historyError = ref(''), historyLoading = ref(false)
let historyGeneration = 0
const historyVersions = computed(() => Object.entries(history.value?.versions || {}).sort(([a], [b]) => Number(b) - Number(a)))
const effectiveCount = computed(() => records.value.filter(r => r.effective && !r.deleted).length)
const checkoutOptions = computed(() => props.checkouts?.length ? props.checkouts : [{ path: props.root }])
const pending = ref<{ request: SecretRequest; reply: SecretReply }>()
const { value: revealed, clear, reveal } = transientSecret()
const revealedField = ref('value')
const revealedRow = ref(''), copiedRow = ref('')
let generation = 0, interaction = 0
let copiedTimer: ReturnType<typeof setTimeout> | undefined
const rowID = (row: SecretRecord) => `${row.resource}/${row.name}`
const fieldID = (row: SecretRecord, field = 'value') => `${rowID(row)}/${field}`
function copyLabel(row: SecretRecord, field = 'value') {
  if (row.type === 'login') return `Copy ${field}: ${row.name}`
  const kind = /(?:^|_)USERNAME$/i.test(row.name) ? 'username' : /(?:^|_)PASSWORD$/i.test(row.name) ? 'password' : 'value'
  return `Copy ${kind}: ${row.name}`
}
function clearValue() { interaction++; clear(); revealedRow.value = ''; copiedRow.value = ''; if (copiedTimer) clearTimeout(copiedTimer); copiedTimer = undefined }
const base = () => ({ root: checkoutRoot.value, scope: scope.value, ...(scope.value === 'branch' && branch.value ? { branch: branch.value } : {}) })
const selectedCheckout = computed(() => props.checkouts?.find(t => t.path === checkoutRoot.value))
const branchName = computed(() => resources.value.find(r => r.id === ((scope.value === 'branch' ? branch.value : '') || context.value?.branch))?.name || selectedCheckout.value?.branch || 'Detached HEAD')
function sourceName(row: SecretRecord) {
  const owner = resources.value.find(r => r.id === row.resource)
  if (row.scope === 'project') return 'Project · shared'
  if (row.scope === 'branch') return `Branch · ${owner?.name || branchName.value}`
  const checkout = props.checkouts?.find(t => t.path === owner?.path)
  return `Worktree · ${checkout?.branch || branchName.value}`
}
const branches = computed(() => resources.value.filter(r => r.kind === 'branch' && r.state === 'active'))
const existingRecord = computed(() => records.value.find(r => r.name === key.value && !r.deleted))
watch(existingRecord, row => { if (row) { entryType.value = row.type === 'login' ? 'login' : 'value'; usernameEnv.value = row.username_env || ''; passwordEnv.value = row.password_env || ''; mapEnvironment.value = Boolean(row.username_env) } })
const ownedVersion = computed(() => records.value.find(r => r.name === key.value && !r.inherited)?.version || 0)
const resource = computed(() => scope.value === 'project' ? context.value?.project : scope.value === 'branch' ? context.value?.branch : context.value?.worktree)
function closeHistory() { historyGeneration++; historyName.value = ''; history.value = undefined; historyError.value = ''; historyLoading.value = false }
function reset() { generation++; closeHistory(); clearValue(); password.value = ''; username.value = ''; pending.value = undefined; history.value = undefined; records.value = [] }
async function load() {
  reset(); if (!state.session.signed_in) return
  const current = generation; busy.value = true; error.value = ''
  try {
    const result = await secretRequest({ operation: 'context', ...base() })
    if (current !== generation) return
    context.value = result.context; resources.value = result.resources || []; emit('context', result)
    const list = await secretRequest({ operation: 'list', ...base() })
    if (current === generation) records.value = list.records || []
  } catch (e) { if (current === generation) error.value = String(e instanceof Error ? e.message : e) }
  finally { if (current === generation) busy.value = false }
}
async function action(request: SecretRequest) {
  busy.value = true; error.value = ''; clearValue(); const current = generation
  try {
    const reply = await secretRequest({ ...base(), ...request })
    if (current !== generation) return
    if (reply.confirmation && !request.confirm) { pending.value = { request, reply }; return }
    pending.value = undefined; message.value = reply.message || 'Saved'; await load()
  } catch (e) { if (current === generation) error.value = String(e instanceof Error ? e.message : e) }
  finally { busy.value = false }
}
async function save(generate: boolean) {
  const value = password.value, loginUsername = username.value
  password.value = ''; username.value = ''
  await action({ operation: generate ? 'generate' : 'set', key: key.value, type: entryType.value,
    ...(entryType.value === 'login' ? { username: loginUsername, password: generate ? undefined : value,
      username_env: mapEnvironment.value ? usernameEnv.value : undefined, password_env: mapEnvironment.value ? passwordEnv.value : undefined, clear_env: !mapEnvironment.value }
      : { value: generate ? undefined : value }), length: 32, expected: ownedVersion.value })
}
async function show(row: SecretRecord, field = 'value') {
  if (revealed.value && revealedRow.value === rowID(row) && revealedField.value === field) { clearValue(); return }
  clearValue(); error.value = ''; revealedRow.value = rowID(row); revealedField.value = field
  const current = interaction
  try { await reveal(async () => (await secretRequest({ operation: 'get', ...base(), key: row.name, field, reveal: true })).value || '') }
  catch (e) { if (current === interaction) error.value = String(e instanceof Error ? e.message : e) }
}
async function copyValue(row: SecretRecord, field = 'value') {
  clearValue(); error.value = ''; message.value = ''
  const current = interaction
  try {
    const reply = await secretRequest({ operation: 'get', ...base(), key: row.name, field, reveal: true })
    if (current !== interaction || !state.session.signed_in) return
    await navigator.clipboard.writeText(reply.value || '')
    if (current !== interaction) return
    copiedRow.value = fieldID(row, field); message.value = `${row.type === 'login' ? `${field === 'username' ? 'Username' : 'Password'} for ` : ''}${row.name} copied. Your clipboard may retain the value.`
    copiedTimer = setTimeout(() => { copiedRow.value = ''; copiedTimer = undefined }, 2000)
  } catch { if (current === interaction) error.value = 'Could not copy this value. Check clipboard permission and try again.' }
}
async function showHistory(row: SecretRecord) {
  clearValue(); closeHistory(); historyName.value = row.name; historyLoading.value = true
  const current = historyGeneration
  try {
    const response = await secretRequest({ operation: 'history', ...base(), key: row.name })
    if (current === historyGeneration) history.value = response.history
  } catch (e) {
    if (current === historyGeneration) historyError.value = String(e instanceof Error ? e.message : e)
  } finally { if (current === historyGeneration) historyLoading.value = false }
}
watch(() => props.root, root => { checkoutRoot.value = root })
watch(checkoutRoot, () => { branch.value = ''; context.value = undefined; resources.value = [] })
watch(() => [checkoutRoot.value, scope.value, branch.value, state.session.signed_in], load, { immediate: true })
watch(() => props.defaultScope, (v) => { scope.value = v })
onDeactivated(reset); onBeforeUnmount(reset)
</script>
<template>
  <section class="secrets-panel workspace-surface" aria-label="Project secrets">
    <header><div><h2>Secrets</h2><p>Save a credential once, then find and use it here.</p></div><Button variant="outline" :disabled="busy" @click="load">Refresh</Button></header>
    <p v-if="!state.session.signed_in">Sign in to view or manage secrets.</p>
    <template v-else>
      <div class="controls scope-controls">
        <label>Scope <select v-model="scope" aria-label="Scope"><option value="project">Project</option><option value="branch">Branch</option><option value="worktree">Worktree</option></select></label>
        <label v-if="scope === 'worktree'" class="checkout-select">Worktree <select v-model="checkoutRoot" aria-label="Worktree"><option v-for="checkout in checkoutOptions" :key="checkout.path" :value="checkout.path">{{ checkout.branch || 'Detached HEAD' }} · {{ checkout.path }}</option></select></label>
        <label v-if="scope === 'branch'">Branch <select v-model="branch" aria-label="Branch"><option value="">{{ selectedCheckout?.branch || 'Current branch' }} · current checkout</option><option v-for="item in branches" :key="item.id" :value="item.id">{{ item.name }}</option></select></label>
        <Button variant="outline" :disabled="busy" @click="action({ operation: 'register' })">Register this checkout</Button>
      </div>
      <div class="context-summary" aria-label="Selected secret context">
        <strong v-if="scope === 'project'">Project secrets · shared across branches and worktrees</strong>
        <strong v-else>{{ scope === 'branch' ? 'Branch' : 'Worktree' }} · {{ branchName }}</strong>
        <span v-if="scope === 'worktree'" class="context-path">{{ checkoutRoot }}</span>
        <span v-if="busy">Loading secrets…</span><span v-else-if="!error">{{ effectiveCount }} effective {{ effectiveCount === 1 ? 'secret' : 'secrets' }}<span v-if="scope !== 'project'"> · includes inherited values</span></span>
      </div>
      <p class="scope-help" v-if="scope === 'worktree'">This checkout inherits project and branch secrets. Worktree-only secrets are not shared back to the branch.</p>
      <p class="scope-help" v-else-if="scope === 'branch'">Shared by worktrees on this branch, with project secrets inherited. Secrets saved only in a worktree do not appear here.</p>
      <p class="scope-help" v-else>Shared by all branches and worktrees. Each can override a shared secret without changing it here.</p>
      <p v-if="error" role="alert">{{ error }}</p><p v-if="message" role="status">{{ message }}</p>
      <h3 class="editor-title">Save a secret</h3>
      <form class="controls secret-editor" @submit.prevent="save(false)">
        <label>Entry type <select v-model="entryType" aria-label="Entry type" :disabled="Boolean(existingRecord)"><option value="value">Single value</option><option value="login">Login (username + password)</option></select></label>
        <label>Name <Input v-model="key" aria-label="Name" autocomplete="off" :placeholder="entryType === 'login' ? 'dev-admin' : 'DATABASE_PASSWORD'" required /></label>
        <label v-if="entryType === 'login'">Username <Input v-model="username" aria-label="Username" autocomplete="off" required /></label>
        <label>{{ entryType === 'login' ? 'Password' : 'Value' }} <Input v-model="password" :aria-label="entryType === 'login' ? 'Password' : 'Value'" type="password" autocomplete="new-password" placeholder="Enter a value or generate one" /></label>
        <Button type="submit" :disabled="busy || !key || !password || (entryType === 'login' && !username)">Save in {{ scope }}</Button><Button type="button" variant="outline" :disabled="busy || !key || (entryType === 'login' && (!username || (mapEnvironment && (!usernameEnv || !passwordEnv))))" @click="save(true)">Generate password</Button>
        <div v-if="entryType === 'login'" class="login-mapping">
          <label class="mapping-toggle"><input v-model="mapEnvironment" type="checkbox" /> Map fields to environment variables</label>
          <div v-if="mapEnvironment" class="controls"><label>Username variable <Input v-model="usernameEnv" aria-label="Username variable" placeholder="APP_USERNAME" required /></label><label>Password variable <Input v-model="passwordEnv" aria-label="Password variable" placeholder="APP_PASSWORD" required /></label></div>
          <small v-else>Available for reveal/copy; not injected into commands.</small>
        </div>
      </form>
      <p class="editor-help">Saving an inherited name creates an override in the selected scope. Rotation changes the stored value; update the consuming application separately.</p>
      <div class="secret-table" role="region" aria-label="Secret values" tabindex="0"><table><thead><tr><th>Name</th><th>Source</th><th>Updated</th><th>Actions</th></tr></thead><tbody>
        <tr v-for="row in records" :key="`${row.resource}/${row.name}`"><td class="secret-name"><code>{{ row.name }}</code><small v-if="row.deleted">Recoverable deletion</small><div v-if="row.type !== 'login' && revealed && revealedRow === rowID(row)" class="row-reveal"><output :aria-label="`Value for ${row.name}`">{{ revealed }}</output><small>Hidden automatically after 30 seconds.</small></div><div v-if="row.type === 'login'" class="login-fields">
          <div v-for="field in loginFields" :key="field" class="login-field">
            <span class="field-label"><component :is="field === 'username' ? UserRound : KeyRound" :size="14" aria-hidden="true" />{{ field === 'username' ? 'Username' : 'Password' }}</span>
            <output v-if="revealed && revealedRow === rowID(row) && revealedField === field" :aria-label="`${field} for ${row.name}`">{{ revealed }}</output><span v-else class="masked-value" aria-label="Hidden">••••••••</span>
            <Button variant="outline" size="icon" :aria-label="`${revealed && revealedRow === rowID(row) && revealedField === field ? 'Hide' : 'Reveal'} ${field}: ${row.name}`" :title="`Reveal/hide ${field}`" :disabled="busy || row.deleted || !row.effective" @click="show(row, field)"><EyeOff v-if="revealed && revealedRow === rowID(row) && revealedField === field" aria-hidden="true" /><Eye v-else aria-hidden="true" /></Button>
            <Button variant="outline" size="icon" :aria-label="copyLabel(row, field)" :title="copiedRow === fieldID(row, field) ? 'Copied' : copyLabel(row, field)" :disabled="busy || row.deleted || !row.effective" @click="copyValue(row, field)"><Check v-if="copiedRow === fieldID(row, field)" aria-hidden="true" /><Copy v-else aria-hidden="true" /></Button>
          </div>
          <small v-if="revealed && revealedRow === rowID(row)">Hidden automatically after 30 seconds.</small>
          <small>{{ row.username_env ? `${row.username_env} / ${row.password_env}` : 'Not mapped to environment variables' }}</small>
        </div></td><td>{{ sourceName(row) }}<small>{{ row.inherited ? 'inherited' : 'owned' }}</small> <small>{{ row.deleted ? 'deleted' : row.effective ? 'effective' : 'overridden' }}</small></td><td class="secret-updated">{{ new Date(row.updated).toLocaleString() }}</td><td><div class="controls row-actions">
          <template v-if="row.type !== 'login'"><Button variant="outline" :disabled="busy || row.deleted || !row.effective" @click="show(row)">{{ revealed && revealedRow === rowID(row) ? 'Hide' : 'Reveal' }}</Button><Button variant="outline" size="icon" :aria-label="copyLabel(row)" :title="copiedRow === fieldID(row) ? 'Copied' : copyLabel(row)" :disabled="busy || row.deleted || !row.effective" @click="copyValue(row)"><Check v-if="copiedRow === fieldID(row)" aria-hidden="true" /><Copy v-else aria-hidden="true" /></Button></template>
          <Button variant="outline" :disabled="busy || row.deleted || !row.effective" @click="showHistory(row)">History</Button>
          <Button variant="outline" :disabled="busy || row.inherited || row.deleted" @click="action({ operation: 'rotate', key: row.name, expected: row.version })">{{ row.type === 'login' ? 'Rotate password' : 'Rotate' }}</Button>
          <Button variant="outline" :disabled="busy || row.inherited" @click="action({ operation: row.deleted ? 'restore' : 'delete', key: row.name })">{{ row.deleted ? 'Restore' : 'Delete' }}</Button>
        </div></td></tr>
        <tr v-if="!records.length"><td colspan="4" class="empty-state">{{ busy ? 'Loading secrets…' : error ? 'Secrets could not be loaded. Try Refresh.' : 'No secrets in this context yet.' }}</td></tr>
      </tbody></table></div>
      <Dialog :open="Boolean(historyName)" @update:open="!$event && closeHistory()">
        <DialogContent class="secret-history-dialog">
          <DialogHeader><DialogTitle>Secret history</DialogTitle><DialogDescription>{{ historyName }} · Version metadata only. Values stay hidden.</DialogDescription></DialogHeader>
          <p v-if="historyLoading" role="status">Loading history…</p>
          <p v-else-if="historyError" role="alert">{{ historyError }}</p>
          <div v-else class="history-list">
            <p v-if="!historyVersions.length">No version history available.</p>
            <div v-for="[version, entry] in historyVersions" :key="version" class="history-version">
              <div><strong>Version {{ version }}</strong><span>{{ entry.destroyed ? 'Destroyed' : entry.deletion_time ? 'Deleted' : Number(version) === history?.current_version ? 'Current' : 'Previous' }}</span></div>
              <time>{{ new Date(entry.created_time).toLocaleString() }}</time>
              <small v-if="entry.deletion_time">Deleted {{ new Date(entry.deletion_time).toLocaleString() }}</small>
            </div>
          </div>
          <DialogFooter><Button variant="outline" @click="closeHistory">Close</Button></DialogFooter>
        </DialogContent>
      </Dialog>
      <footer class="scope-footer"><Button variant="ghost" :disabled="busy || !resource" @click="action({ operation: 'cleanup-retire', resource })">Preview retiring this secret scope</Button></footer>
      <div v-if="pending" class="confirmation" role="dialog" aria-label="Confirm secret change"><h3>Review secret change</h3><p>{{ pending.reply.message }}</p><ul><li v-for="row in pending.reply.records || []" :key="`${row.resource}/${row.name}`">{{ row.name }} — {{ row.scope }}</li><li v-for="item in pending.reply.resources || []" :key="item.id">{{ item.name }} — {{ item.deadline ? `retained until ${new Date(item.deadline).toLocaleString()}` : 'no expiry shown' }}</li></ul><Button :disabled="busy" @click="action({ ...pending.request, confirm: pending.reply.confirmation })">Confirm</Button> <Button variant="outline" @click="pending = undefined">Cancel</Button></div>
    </template>
  </section>
</template>
<style scoped>
.secrets-panel{display:grid;grid-template-columns:minmax(0,1fr);gap:1rem;min-width:0;padding:1.5rem}.context-summary{display:grid;gap:.3rem;padding:.75rem 1rem;background:var(--background);border-radius:.35rem}.context-summary span{font-size:.85rem}.context-path{font-family:monospace;overflow-wrap:anywhere}.controls select{width:100%;min-width:0;max-width:100%}.controls label{min-width:0;max-width:100%}.secret-table{min-width:0;max-width:100%;overflow-x:auto}.secret-table:focus-visible{outline:2px solid var(--ring);outline-offset:2px}.secrets-panel header,.controls{display:flex;gap:.75rem;align-items:end;flex-wrap:wrap}.secrets-panel header{justify-content:space-between}.controls label{display:grid;gap:.3rem}select{min-height:2.5rem;padding:.5rem .75rem;border:1px solid var(--border);border-radius:.35rem;background:var(--background);color:inherit}table{width:100%;border-collapse:collapse}th,td{text-align:left;vertical-align:top;padding:1rem;border-bottom:1px solid var(--border)}small{display:block;color:var(--muted-foreground)}.confirmation{padding:1rem;border:1px solid var(--border);border-radius:.5rem}.secret-name{min-width:12rem}.row-reveal{margin-top:.6rem;max-width:32rem;white-space:normal}.row-reveal output{display:block;font-family:monospace;overflow-wrap:anywhere;user-select:all}.row-reveal small{margin-top:.35rem}pre{white-space:pre-wrap}p{margin:0}h2,h3{margin:0}
.login-mapping{width:100%;display:grid;gap:.5rem}.controls .mapping-toggle{display:flex;gap:.5rem;align-items:center}.login-fields{display:grid;gap:.4rem;margin-top:.6rem}.login-field{display:grid;grid-template-columns:6rem minmax(5rem,1fr) 2.5rem 2.5rem;align-items:center;gap:.4rem}.field-label{display:flex;align-items:center;gap:.35rem;font-size:.8rem}.login-field output{font-family:monospace;overflow-wrap:anywhere;white-space:normal;user-select:all}.masked-value{letter-spacing:.12em}.secret-name:has(.login-fields){min-width:24rem}
.secrets-panel header{align-items:center;padding-bottom:.5rem}.secrets-panel h2{font-size:1.125rem;font-weight:600}.secrets-panel header p,.scope-help,.editor-help{color:var(--muted-foreground);font-size:.875rem;line-height:1.6}.scope-controls{align-items:end}.scope-controls label{min-width:10rem}.scope-controls .checkout-select{flex:1;max-width:40rem}.controls label{font-size:.875rem;font-weight:500}.context-summary{padding:1rem 1.25rem}.context-summary span{color:var(--muted-foreground)}.editor-title{font-size:1rem;font-weight:600;border-top:1px solid var(--border);padding-top:1.5rem;margin-top:.25rem}.secret-editor>label{flex:1 1 12rem}.secret-editor{gap:1rem}.editor-help{max-width:85ch}.secret-table{border:1px solid var(--border);border-radius:.5rem}.secret-table th{font-size:.75rem;font-weight:600;color:var(--muted-foreground);background:var(--muted)}.secret-table tbody tr:last-child td{border-bottom:0}.row-actions{gap:.5rem;align-items:center}.secret-name code{overflow-wrap:anywhere}.secret-table .empty-state{text-align:center;padding:2.5rem 1rem;color:var(--muted-foreground)}.scope-footer{border-top:1px solid var(--border);padding-top:.75rem}.scope-footer button{white-space:normal;text-align:left}.secret-history-dialog{max-width:36rem}.history-list{max-height:55vh;overflow:auto}.history-version{padding:1rem 0;border-bottom:1px solid var(--border)}.history-version:first-child{padding-top:0}.history-version>div{display:flex;justify-content:space-between;gap:1rem}.history-version span,.history-version time{font-size:.875rem;color:var(--muted-foreground)}.history-version time{display:block;margin-top:.35rem}select:focus-visible{outline:2px solid var(--ring);outline-offset:2px}
@media(max-width:640px){.secrets-panel{padding:1rem}.scope-controls>label,.scope-controls .checkout-select{flex:1 1 100%;width:100%;min-width:0}.secret-editor>label{flex-basis:100%}.secret-table{border:0;border-radius:0}.secret-history-dialog{max-width:calc(100vw - 2rem)}.secret-table{overflow:visible}table,tbody,tr,td{display:block;width:100%}thead{display:none}tr{padding:.75rem;border-bottom:1px solid var(--border)}td{border:0;padding:.3rem 0}.secret-name,.secret-name:has(.login-fields){min-width:0}.login-field{grid-template-columns:5rem minmax(0,1fr) 2.5rem 2.5rem}.secret-updated{font-size:.8rem}.secret-updated::before{content:'Updated ';opacity:.7}.row-actions{gap:.5rem}.row-reveal{max-width:100%}}
</style>
