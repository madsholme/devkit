<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { BookOpen, FileText, Plus, Pencil, Save, RefreshCw } from 'lucide-vue-next'
import { Button } from '@madsholme/ui/button'
import { Input } from '@madsholme/ui/input'
import { Textarea } from '@madsholme/ui/textarea'
import { Badge } from '@madsholme/ui/badge'
import { request, post, APIError } from '../api'
import { state, openDialog, refresh } from '../dashboard'
import type { Checkout } from '../types'
import FilterSelect from './FilterSelect.vue'
import InventoryNotice from './InventoryNotice.vue'
import MarkdownDocument from './MarkdownDocument.vue'
interface Entry {
  path: string
  editable: boolean
}
interface Document extends Entry {
  content: string
  version: string
}
const props = defineProps<{ checkouts: Checkout[]; main: string }>()
const route = useRoute()
const router = useRouter()
const checkout = computed(() =>
  typeof route.query.tree === 'string' ? route.query.tree : props.main
)
const options = computed(() =>
  props.checkouts.map((t) => ({
    value: t.path,
    label: `${t.branch || 'Project folder'}${
      t.path === props.main ? ' · main checkout' : ''
    } · ${t.path.split('/').pop()}`,
  }))
)
const files = ref<Entry[]>([])
const warnings = ref<string[]>([])
const filter = ref('')
const filtered = computed(() =>
  files.value.filter((f) => f.path.toLowerCase().includes(filter.value.toLowerCase()))
)
const document = ref<Document | null>(null)
const error = ref('')
const notice = ref('')
const loading = ref(false)
const saving = ref(false)
const editing = ref(false)
const creating = ref(false)
const preview = ref(false)
const draft = ref('')
const newPath = ref('')
const draftBytes = computed(() => new TextEncoder().encode(draft.value).length)
const dirty = computed(
  () =>
    editing.value &&
    (creating.value
      ? Boolean(newPath.value || draft.value)
      : draft.value !== document.value?.content)
)
const editorPath = computed(() =>
  creating.value ? `docs/${newPath.value}` : document.value?.path || ''
)
const renderer = ref<InstanceType<typeof MarkdownDocument>>()
let generation = 0
let alive = true
function canLeave() {
  if (saving.value) return false
  if (dirty.value && !window.confirm('Discard your unsaved document changes?')) return false
  return true
}
function leaveRoute() {
  if (!canLeave()) return false
  editing.value = false
  creating.value = false
  return true
}
onBeforeRouteLeave(leaveRoute)
onBeforeRouteUpdate(leaveRoute)
function beforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value || saving.value) {
    e.preventDefault()
    e.returnValue = ''
  }
}
window.addEventListener('beforeunload', beforeUnload)
onBeforeUnmount(() => {
  alive = false
  generation++
  window.removeEventListener('beforeunload', beforeUnload)
})
function navigate(path: string, anchor = '') {
  void router.push({ query: { ...route.query, doc: path, anchor: anchor || undefined } })
}
function changeCheckout(value: string) {
  void router.push({ query: { tree: value } })
}
async function load() {
  const current = ++generation
  loading.value = true
  error.value = ''
  document.value = null
  editing.value = false
  creating.value = false
  if (!props.checkouts.some((t) => t.path === checkout.value)) {
    error.value = 'This checkout is no longer in the project inventory.'
    loading.value = false
    files.value = []
    return
  }
  try {
    const query = new URLSearchParams({ worktree: checkout.value })
    const data = await request<{ files: Entry[]; warnings: string[] }>(`documents?${query}`)
    if (current !== generation) return
    files.value = data.files
    warnings.value = data.warnings
    const selected =
      typeof route.query.doc === 'string'
        ? route.query.doc
        : data.files.find((f) => f.path.startsWith('docs/'))?.path || data.files[0]?.path
    if (!selected) return
    query.set('path', selected)
    const result = await request<Document>(`document?${query}`)
    if (current !== generation) return
    document.value = result
    loading.value = false
    await nextTick()
    if (typeof route.query.anchor === 'string') renderer.value?.scrollTo(route.query.anchor)
  } catch (e) {
    if (current === generation) error.value = e instanceof Error ? e.message : String(e)
  } finally {
    if (current === generation) loading.value = false
  }
}
watch(
  () => [checkout.value, route.query.doc, route.query.anchor],
  () => {
    notice.value = ''
    void load()
  },
  { immediate: true }
)
function startEdit() {
  if (!state.session.signed_in) {
    openDialog('signin')
    return
  }
  if (!document.value?.editable) return
  draft.value = document.value.content
  creating.value = false
  editing.value = true
  preview.value = false
  notice.value = ''
  error.value = ''
}
function startNew() {
  if (!state.session.signed_in) {
    openDialog('signin')
    return
  }
  if (!canLeave()) return
  creating.value = true
  editing.value = true
  draft.value = ''
  newPath.value = ''
  preview.value = false
  notice.value = ''
  error.value = ''
}
function cancelEdit() {
  if (canLeave()) {
    editing.value = false
    creating.value = false
    error.value = ''
  }
}
async function reload() {
  if (canLeave()) {
    notice.value = ''
    await load()
  }
}
async function save() {
  if (saving.value || draftBytes.value > 262144) return
  saving.value = true
  error.value = ''
  notice.value = ''
  const path = editorPath.value
  try {
    const job = await post<{ id: string }>('action', {
      action: 'document-save',
      worktree: checkout.value,
      path,
      expected: creating.value ? 'new' : document.value?.version,
      content: draft.value,
    })
    let complete = false
    for (let i = 0; i < 120 && alive; i++) {
      const result = await request<{ done: boolean; ok: boolean; output: string }>(
        `job?id=${encodeURIComponent(job.id)}`
      )
      if (result.done) {
        if (!result.ok)
          throw new Error(
            result.output
              .split('\n')
              .filter((line) => !line.startsWith('$ devkit'))
              .join('\n')
              .trim()
          )
        complete = true
        break
      }
      await new Promise((resolve) => setTimeout(resolve, 500))
    }
    if (!complete)
      throw new Error(
        'The save is still running or its status is unavailable. Keep your draft and reload the document before retrying.'
      )
    editing.value = false
    creating.value = false
    saving.value = false
    await router.replace({ query: { ...route.query, doc: path } })
    await load()
    notice.value = `Saved ${path}. Changes remain uncommitted.`
    void refresh()
  } catch (e) {
    if (e instanceof APIError && e.status === 401) {
      openDialog('signin')
      error.value = 'Sign in again, then save. Your draft is still here.'
    } else error.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}
</script>
<template>
  <section class="project-documents" aria-label="Project documents">
    <div class="document-toolbar">
      <div class="document-checkout">
        <label>Checkout</label
        ><FilterSelect
          :model-value="checkout"
          label="Document checkout"
          :options="options"
          @update:model-value="changeCheckout"
        />
      </div>
      <div class="document-toolbar-actions">
        <Button variant="outline" :disabled="saving || loading" @click="reload"
          ><RefreshCw :size="15" />Reload</Button
        ><Button :disabled="saving" @click="startNew"><Plus :size="16" />New document</Button>
      </div>
    </div>
    <p class="note document-checkout-path">{{ checkout }}</p>
    <InventoryNotice :text="error" error /><InventoryNotice
      v-for="warning in warnings"
      :key="warning"
      :text="warning"
    />
    <p v-if="notice" role="status" class="document-save-notice">{{ notice }}</p>
    <div class="documents-layout">
      <aside class="workspace-surface document-sidebar" aria-label="Document files">
        <div class="panel-heading">
          <BookOpen :size="17" />
          <h2>Documents</h2>
          <span class="note">{{ files.length }}</span>
        </div>
        <div class="document-filter">
          <Input
            v-model="filter"
            placeholder="Find a plan or document…"
            aria-label="Filter documents"
          />
        </div>
        <nav aria-label="Markdown files">
          <button
            v-for="file in filtered"
            :key="file.path"
            type="button"
            :disabled="saving"
            :aria-current="!creating && document?.path === file.path ? 'page' : undefined"
            @click="navigate(file.path)"
          >
            <FileText :size="15" /><span>{{ file.path }}</span
            ><span v-if="!file.editable" class="sr-only">Read-only agent guidance</span>
          </button>
        </nav>
        <p v-if="!filtered.length && !loading" class="document-empty note">
          {{
            files.length
              ? 'No matching documents.'
              : 'No Markdown files yet. Create a plan in docs/ to get started.'
          }}
        </p>
      </aside>
      <section class="workspace-surface document-pane">
        <template v-if="editing">
          <header class="document-heading">
            <div>
              <h2>{{ creating ? 'New document' : document?.path }}</h2>
              <p class="note">
                {{ dirty ? 'Unsaved changes' : 'Editing Markdown' }} ·
                {{ draftBytes.toLocaleString() }} / 262,144 bytes
              </p>
            </div>
            <Badge v-if="dirty" variant="warning">Unsaved</Badge>
          </header>
          <form class="document-edit-form" @submit.prevent="save">
            <div v-if="creating" class="document-new-path">
              <label for="document-path">File path inside docs/</label
              ><Input
                id="document-path"
                v-model="newPath"
                placeholder="plans/my-plan.md"
                required
                maxlength="507"
                :disabled="saving"
              />
              <p class="note">Use .md. New folders are created as needed.</p>
            </div>
            <div class="document-editor-actions">
              <Button
                type="button"
                variant="outline"
                :disabled="saving"
                @click="preview = !preview"
                >{{ preview ? 'Edit source' : 'Preview Markdown' }}</Button
              ><span class="note">Saves to this checkout without committing.</span>
            </div>
            <MarkdownDocument v-if="preview" :content="draft" :path="editorPath" />
            <Textarea
              v-else
              v-model="draft"
              aria-label="Markdown source"
              class="document-source"
              :disabled="saving"
              spellcheck="false"
              :aria-invalid="draftBytes > 262144"
            />
            <div class="document-save-actions">
              <Button type="button" variant="outline" :disabled="saving" @click="cancelEdit"
                >Cancel</Button
              ><Button
                type="submit"
                :disabled="saving || draftBytes > 262144 || (creating && !newPath)"
                ><Save :size="15" />{{ saving ? 'Saving…' : 'Save document' }}</Button
              >
            </div>
          </form>
        </template>
        <template v-else-if="document && !loading">
          <header class="document-heading">
            <div>
              <h2>{{ document.path }}</h2>
              <p class="note">
                {{ document.editable ? 'Working tree document' : 'Agent guidance · read-only' }}
              </p>
            </div>
            <Button v-if="document.editable" variant="outline" @click="startEdit"
              ><Pencil :size="15" />Edit</Button
            >
          </header>
          <div class="document-body">
            <MarkdownDocument
              ref="renderer"
              :content="document.content"
              :path="document.path"
              @navigate="navigate"
            />
          </div>
        </template>
        <div v-else class="empty-state">
          <BookOpen :size="28" />
          <h2>
            {{
              loading
                ? 'Loading documents…'
                : error
                ? 'Document unavailable'
                : 'Plans, specs and guidance'
            }}
          </h2>
          <p>
            {{
              loading
                ? 'Reading this checkout’s Markdown files.'
                : 'Choose a file to read, or create a new document under docs/.'
            }}
          </p>
        </div>
      </section>
    </div>
  </section>
</template>
<style scoped>
.document-toolbar,
.document-toolbar-actions,
.document-heading,
.document-editor-actions,
.document-save-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.document-toolbar {
  justify-content: space-between;
  margin: 1.25rem 0;
}
.document-checkout {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-width: 0;
}
.document-checkout label {
  font-size: 0.8125rem;
  color: var(--muted-foreground);
}
.document-checkout :deep(button) {
  width: min(27rem, calc(100vw - 8rem));
  max-width: 100%;
}
.document-checkout-path {
  margin: -0.5rem 0 1rem;
  overflow-wrap: anywhere;
}
.documents-layout {
  display: grid;
  grid-template-columns: minmax(13rem, 17rem) minmax(0, 1fr);
  gap: 1.25rem;
  align-items: start;
}
.document-sidebar {
  overflow: hidden;
}
.document-filter {
  padding: 0 0.75rem 0.75rem;
}
.document-sidebar nav {
  display: grid;
  padding: 0 0.5rem 0.5rem;
  max-height: 65vh;
  overflow-y: auto;
}
.document-sidebar nav button {
  display: flex;
  align-items: start;
  gap: 0.5rem;
  padding: 0.65rem 0.5rem;
  border-radius: var(--radius);
  font-size: 0.8125rem;
  text-align: left;
  overflow-wrap: anywhere;
}
.document-sidebar nav button svg {
  flex-shrink: 0;
  margin-top: 0.1rem;
}
.document-sidebar nav button:hover {
  background: var(--muted);
}
.document-sidebar nav button[aria-current] {
  background: var(--accent);
  color: var(--accent-foreground);
  font-weight: 600;
}
.document-empty {
  padding: 0.75rem 1rem 1rem;
}
.document-pane {
  min-width: 0;
  overflow: hidden;
}
.document-heading {
  justify-content: space-between;
  border-bottom: 1px solid var(--border);
  padding: 1rem 1.5rem;
}
.document-heading h2 {
  font-size: 0.9375rem;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.document-heading .note {
  margin-top: 0.25rem;
}
.document-body,
.document-edit-form {
  padding: 1.75rem clamp(1rem, 3vw, 3rem);
}
.document-edit-form {
  display: grid;
  gap: 1rem;
}
.document-new-path {
  display: grid;
  gap: 0.5rem;
}
.document-new-path label {
  font-size: 0.8125rem;
  font-weight: 500;
}
.document-source {
  min-height: 26rem;
  font: 0.8125rem/1.7 ui-monospace, monospace;
  tab-size: 2;
}
.document-save-actions {
  justify-content: flex-end;
}
.document-save-notice {
  color: var(--primary);
  font-size: 0.875rem;
  margin-bottom: 1rem;
}
@media (max-width: 760px) {
  .documents-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .document-sidebar nav {
    max-height: 13rem;
  }
  .document-toolbar {
    align-items: stretch;
  }
  .document-heading {
    padding: 1rem;
  }
  .document-body,
  .document-edit-form {
    padding: 1.25rem 1rem;
  }
}
</style>
