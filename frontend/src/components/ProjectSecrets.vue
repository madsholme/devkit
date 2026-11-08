<script setup lang="ts">
import { computed, onBeforeUnmount, onDeactivated, ref, watch } from 'vue'
import { Button } from '@madsholme/ui/button'
import { Input } from '@madsholme/ui/input'
import { state } from '../dashboard'
import { secretRequest, transientSecret, type SecretRecord, type SecretReply, type SecretRequest } from '../secrets'
const props = withDefaults(defineProps<{ root: string; defaultScope?: string }>(), { defaultScope: 'project' })
const scope = ref(props.defaultScope), branch = ref(''), key = ref(''), password = ref('')
const records = ref<SecretRecord[]>([]), context = ref<SecretReply['context']>(), resources = ref<NonNullable<SecretReply['resources']>>([])
const busy = ref(false), error = ref(''), message = ref(''), history = ref<unknown>()
const pending = ref<{ request: SecretRequest; reply: SecretReply }>()
const { value: revealed, clear, reveal } = transientSecret()
let generation = 0
const base = () => ({ root: props.root, scope: scope.value, ...(scope.value === 'branch' && branch.value ? { branch: branch.value } : {}) })
const branches = computed(() => resources.value.filter(r => r.kind === 'branch' && r.state === 'active'))
const ownedVersion = computed(() => records.value.find(r => r.name === key.value && !r.inherited)?.version || 0)
const resource = computed(() => scope.value === 'project' ? context.value?.project : scope.value === 'branch' ? context.value?.branch : context.value?.worktree)
function reset() { generation++; clear(); password.value = ''; pending.value = undefined; history.value = undefined; records.value = [] }
async function load() {
  reset(); if (!state.session.signed_in) return
  const current = generation; busy.value = true; error.value = ''
  try {
    const result = await secretRequest({ operation: 'context', ...base() })
    if (current !== generation) return
    context.value = result.context; resources.value = result.resources || []
    const list = await secretRequest({ operation: 'list', ...base() })
    if (current === generation) records.value = list.records || []
  } catch (e) { if (current === generation) error.value = String(e instanceof Error ? e.message : e) }
  finally { if (current === generation) busy.value = false }
}
async function action(request: SecretRequest) {
  busy.value = true; error.value = ''; clear(); const current = generation
  try {
    const reply = await secretRequest({ ...base(), ...request })
    if (current !== generation) return
    if (reply.confirmation && !request.confirm) { pending.value = { request, reply }; return }
    pending.value = undefined; message.value = reply.message || 'Saved'; await load()
  } catch (e) { if (current === generation) error.value = String(e instanceof Error ? e.message : e) }
  finally { busy.value = false }
}
async function save(generate: boolean) {
  const value = password.value; password.value = ''
  await action({ operation: generate ? 'generate' : 'set', key: key.value, value: generate ? undefined : value, length: 32, expected: ownedVersion.value })
}
async function show(row: SecretRecord, copy = false) {
  error.value = ''
  try {
    await reveal(async () => (await secretRequest({ operation: 'get', ...base(), key: row.name, reveal: true })).value || '')
    if (copy && revealed.value) { await navigator.clipboard.writeText(revealed.value); message.value = 'Copied. Your clipboard may retain the value.' }
  } catch (e) { error.value = String(e instanceof Error ? e.message : e) }
}
async function showHistory(row: SecretRecord) {
  clear(); const current = generation
  try { const response = await secretRequest({ operation: 'history', ...base(), key: row.name }); if (current === generation) history.value = response.history }
  catch (e) { error.value = String(e instanceof Error ? e.message : e) }
}
watch(() => [props.root, scope.value, branch.value, state.session.signed_in], load, { immediate: true })
watch(() => props.defaultScope, (v) => { scope.value = v })
onDeactivated(reset); onBeforeUnmount(reset)
</script>
<template>
  <section class="secrets-panel workspace-surface" aria-label="Project secrets">
    <header><div><h2>Secrets</h2><p>Save a credential once, then find and use it here.</p></div><Button variant="outline" :disabled="busy" @click="load">Refresh</Button></header>
    <p v-if="!state.session.signed_in">Sign in to view or manage secrets.</p>
    <template v-else>
      <div class="controls">
        <label>Scope <select v-model="scope"><option value="project">Project</option><option value="branch">Branch</option><option value="worktree">Worktree</option></select></label>
        <label v-if="scope === 'branch'">Branch <select v-model="branch"><option value="">Current branch</option><option v-for="item in branches" :key="item.id" :value="item.id">{{ item.name }}</option></select></label>
        <Button variant="outline" :disabled="busy" @click="action({ operation: 'register' })">Register this checkout</Button>
      </div>
      <p v-if="error" role="alert">{{ error }}</p><p v-if="message" role="status">{{ message }}</p>
      <form class="controls" @submit.prevent="save(false)">
        <label>Name <Input v-model="key" autocomplete="off" placeholder="DATABASE_PASSWORD" required /></label>
        <label>Value <Input v-model="password" type="password" autocomplete="new-password" placeholder="Enter a value or generate one" /></label>
        <Button type="submit" :disabled="busy || !key || !password">Save in {{ scope }}</Button><Button type="button" variant="outline" :disabled="busy || !key" @click="save(true)">Generate password</Button>
      </form>
      <p>Saving an inherited name creates an override in the selected scope. Rotation changes the stored value; update the consuming application separately.</p>
      <div v-if="revealed" class="reveal" role="status"><output>{{ revealed }}</output><Button variant="outline" @click="clear">Hide</Button><small>Hidden automatically after 30 seconds.</small></div>
      <table><thead><tr><th>Name</th><th>Source</th><th>Updated</th><th>Actions</th></tr></thead><tbody>
        <tr v-for="row in records" :key="`${row.resource}/${row.name}`"><td><code>{{ row.name }}</code><small v-if="row.deleted">Recoverable deletion</small></td><td>{{ row.scope }} · {{ row.inherited ? 'inherited' : 'owned' }} <small>{{ row.effective ? 'effective' : 'overridden' }}</small></td><td>{{ new Date(row.updated).toLocaleString() }}</td><td class="controls">
          <Button variant="outline" :disabled="busy || row.deleted || !row.effective" @click="show(row)">Reveal</Button><Button variant="outline" :disabled="busy || row.deleted || !row.effective" @click="show(row, true)">Copy</Button>
          <Button variant="outline" :disabled="busy || row.deleted || !row.effective" @click="showHistory(row)">History</Button>
          <Button variant="outline" :disabled="busy || row.inherited || row.deleted" @click="action({ operation: 'rotate', key: row.name, expected: row.version })">Rotate</Button>
          <Button variant="outline" :disabled="busy || row.inherited" @click="action({ operation: row.deleted ? 'restore' : 'delete', key: row.name })">{{ row.deleted ? 'Restore' : 'Delete' }}</Button>
        </td></tr>
        <tr v-if="!records.length"><td colspan="4">No secrets in this context yet.</td></tr>
      </tbody></table>
      <details v-if="history"><summary>Version history (values hidden)</summary><pre>{{ JSON.stringify(history, null, 2) }}</pre></details>
      <Button variant="outline" :disabled="busy || !resource" @click="action({ operation: 'cleanup-retire', resource })">Preview retiring this secret scope</Button>
      <div v-if="pending" class="confirmation" role="dialog" aria-label="Confirm secret change"><h3>Review secret change</h3><p>{{ pending.reply.message }}</p><ul><li v-for="row in pending.reply.records || []" :key="`${row.resource}/${row.name}`">{{ row.name }} — {{ row.scope }}</li><li v-for="item in pending.reply.resources || []" :key="item.id">{{ item.name }} — {{ item.deadline ? `retained until ${new Date(item.deadline).toLocaleString()}` : 'no expiry shown' }}</li></ul><Button :disabled="busy" @click="action({ ...pending.request, confirm: pending.reply.confirmation })">Confirm</Button> <Button variant="outline" @click="pending = undefined">Cancel</Button></div>
    </template>
  </section>
</template>
<style scoped>
.secrets-panel{display:grid;gap:1rem}.secrets-panel header,.controls{display:flex;gap:.75rem;align-items:end;flex-wrap:wrap}.secrets-panel header{justify-content:space-between}.controls label{display:grid;gap:.3rem}select{padding:.45rem;border:1px solid var(--border);border-radius:.35rem;background:var(--background);color:inherit}table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:.6rem;border-bottom:1px solid var(--border)}small{display:block;opacity:.7}.reveal,.confirmation{padding:1rem;border:1px solid var(--border);border-radius:.5rem}.reveal output{font-family:monospace;overflow-wrap:anywhere;user-select:all}pre{white-space:pre-wrap}p{margin:0}h2,h3{margin:0}
</style>
