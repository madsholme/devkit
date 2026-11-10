<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  ChevronRight,
  ExternalLink,
  Container as ContainerIcon,
  Search,
  MoreHorizontal,
} from 'lucide-vue-next'
import { Badge } from '@madsholme/ui/badge'
import { Button } from '@madsholme/ui/button'
import { Input } from '@madsholme/ui/input'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuLabel,
} from '@madsholme/ui/dropdown-menu'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { filters, state, act, showDetails, showLogs } from '../dashboard'
import { ago, home, safeURL, short, up, wordsMatch, historyVerb } from '../format'
import StatusBadge from '../components/StatusBadge.vue'
import CopyButton from '../components/CopyButton.vue'
import FilterSelect from '../components/FilterSelect.vue'
import ActionButton from '../components/ActionButton.vue'
import type { Preview } from '../types'
import { projectInventories } from '../projects'
import ProjectSecrets from '../components/ProjectSecrets.vue'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@madsholme/ui/dialog'
const expanded = reactive(new Set<string>())
const router = useRouter()
const rows = computed(() =>
  (state.overview?.previews || []).filter(
    (p) =>
      (!filters.mode || p.mode === filters.mode) &&
      (!filters.status || up(p.status) === (filters.status === 'up')) &&
      wordsMatch(filters.previews, [
        p.name,
        p.project,
        p.mode,
        p.status,
        p.branch,
        p.commit,
        p.head,
        p.source_branch,
        p.worktree,
        p.url,
        p.share ? `public shared ${p.share.url}` : '',
        ...p.services.flatMap((s) => [s.name, s.image]),
      ])
  )
)
const orphans = computed(() => (state.overview?.repositories || []).filter((r) => r.orphans.length))
const gone = computed(() => new Set(orphans.value.flatMap((r) => r.orphans.map((o) => o.name))))
const recent = (preview: string) => state.history.filter((e) => e.preview === preview)
function toggleDetails(name: string) {
  expanded.has(name) ? expanded.delete(name) : expanded.add(name)
}
function secretsCheckout(preview: Preview) {
  const owners = projectInventories.value.filter(inventory =>
    inventory.checkouts.some(checkout => checkout.path === preview.worktree)
  )
  return owners.length === 1
    ? owners[0]!.checkouts.find(checkout => checkout.path === preview.worktree)
    : undefined
}
const secretsPreview = ref<Preview>()
const selectedSecretsCheckout = computed(() => secretsPreview.value && secretsCheckout(secretsPreview.value))
watch(() => [state.session.signed_in, selectedSecretsCheckout.value], () => {
  if (!state.session.signed_in || !selectedSecretsCheckout.value) secretsPreview.value = undefined
})
function allEvents(preview: string) {
  filters.history = preview
  void router.push('/history')
}
function routeLabel(url: string, preview: Preview) {
  const host = preview.url.replace(/^https?:\/\//, '')
  const rest = url.replace(/^https?:\/\//, '')
  return rest === host
    ? '/'
    : rest.startsWith(host + '/')
    ? rest.slice(host.length)
    : rest.replace(host, preview.name)
}
function timeLeft(expires: string) {
  const hours = Math.max(0, (Date.parse(expires) - Date.now()) / 3600000)
  return hours < 1 ? `${Math.round(hours * 60)}m left` : `${Math.round(hours)}h left`
}
</script>
<template>
  <div v-if="orphans.length" class="notice">
    <div>
      <strong>Previews with removed worktrees</strong>
      <p>{{ orphans.flatMap((r) => r.orphans.map((o) => o.name)).join(', ') }}</p>
      <div class="actions">
        <ActionButton
          v-for="repo in orphans"
          :key="repo.path"
          :request="{ action: 'gc', repo: repo.path }"
          :label="`Remove ${repo.orphans.length} in ${home(repo.path.replace(/\/\.git$/, ''))}…`"
        />
      </div>
      <p v-if="!state.session.signed_in" class="note">
        Remove with devkit gc --yes in the repository.
      </p>
    </div>
  </div>
  <div class="workspace-surface previews-inventory" data-inventory>
    <div class="workspace-toolbar">
      <div class="search-field">
        <Search :size="16" /><Input
          class="pl-9"
          id="preview-search"
          v-model="filters.previews"
          aria-label="Filter previews"
          placeholder="Filter name, project, branch or commit…"
          @keydown.esc="filters.previews = ''"
        />
      </div>
      <FilterSelect
        v-model="filters.mode"
        label="Preview mode"
        :options="[
          { value: '', label: 'All modes' },
          { value: 'live', label: 'Live' },
          { value: 'review', label: 'Review' },
        ]"
      /><FilterSelect
        v-model="filters.status"
        label="Preview status"
        :options="[
          { value: '', label: 'Any status' },
          { value: 'up', label: 'Running' },
          { value: 'down', label: 'Not running' },
        ]"
      />
    </div>
    <Table class="preview-table"
      ><TableHeader
        ><TableRow
          ><TableHead>Preview</TableHead><TableHead>Status</TableHead><TableHead>Source</TableHead
          ><TableHead>Uptime</TableHead><TableHead class="text-right">Actions</TableHead></TableRow
        ></TableHeader
      ><TableBody
        ><template v-for="(preview, index) in rows" :key="preview.name"
          ><TableRow
            v-if="index === 0 || rows[index - 1]?.project !== preview.project"
            class="group-row"
            ><TableCell colspan="5">{{ preview.project }}</TableCell></TableRow
          ><TableRow class="preview-row"
            ><TableCell class="preview-identity"
              ><div class="resource-identity">
                <Button
                  size="icon"
                  variant="ghost"
                  :aria-label="`Show containers for ${preview.name}`"
                  :aria-expanded="expanded.has(preview.name)"
                  @click="toggleDetails(preview.name)"
                  ><ChevronRight
                    :size="16"
                    :class="{ rotated: expanded.has(preview.name) }" /></Button
                ><Badge variant="secondary">{{ preview.mode }}</Badge
                ><button
                  type="button"
                  class="preview-name text-link font-semibold"
                  :aria-expanded="expanded.has(preview.name)"
                  @click="toggleDetails(preview.name)"
                  >{{ preview.name }}</button>
              </div>
              <p class="path table-path">{{ home(preview.worktree) }}</p>
              <Badge v-if="gone.has(preview.name)" variant="warning"
                >Worktree gone</Badge
              ></TableCell
            ><TableCell class="preview-status"
              ><StatusBadge :status="preview.status" />
              <p v-if="!expanded.has(preview.name)" class="note">
                {{
                  preview.services
                    .filter((s) => !s.web && !(preview.share && s.name === 'share'))
                    .map((s) => `${s.name} ${s.status}`)
                    .join(' · ')
                }}
              </p></TableCell
            ><TableCell class="preview-source" data-label="Source"
              ><code
                >{{
                  preview.mode === 'live'
                    ? preview.branch || 'detached'
                    : preview.source_branch
                    ? `from ${preview.source_branch}`
                    : 'detached'
                }}
                ·
                {{
                  short(preview.mode === 'live' ? preview.head || preview.commit : preview.commit)
                }}</code
              >
              <p v-if="preview.mode === 'live' && preview.changes" class="note">
                <Badge variant="warning">{{ preview.changes }} uncommitted changes</Badge>
              </p>
              <p v-if="preview.mode !== 'live' && preview.behind" class="note">
                <Badge variant="warning">{{ preview.behind }} commits behind</Badge>
              </p></TableCell
            ><TableCell class="preview-uptime" data-label="Uptime">{{ ago(preview.started) }}</TableCell
            ><TableCell class="preview-actions"
              ><div class="actions">
                <DropdownMenu v-if="preview.share"
                  ><DropdownMenuTrigger as-child
                    ><Button
                      variant="outline"
                      size="sm"
                      class="share-button"
                      :title="`Public until ${new Date(preview.share.expires).toLocaleString()}`"
                      >Public · {{ timeLeft(preview.share.expires) }}</Button
                    ></DropdownMenuTrigger
                  ><DropdownMenuContent align="end"
                    ><DropdownMenuLabel>{{ preview.share.url }}</DropdownMenuLabel
                    ><DropdownMenuItem as-child
                      ><a :href="safeURL(preview.share.url)" target="_blank" rel="noopener"
                        >Open public address</a
                      ></DropdownMenuItem
                    ><DropdownMenuItem
                      v-if="state.session.signed_in"
                      @select="act({ action: 'share', preview: preview.name })"
                      >Extend…</DropdownMenuItem
                    ><DropdownMenuItem
                      v-if="state.session.signed_in"
                      @select="act({ action: 'unshare', preview: preview.name })"
                      >Stop sharing</DropdownMenuItem
                    >
                    <div class="flex items-center px-2 text-sm">
                      Copy public address<CopyButton
                        :text="preview.share.url"
                        label="Copy public address"
                      /></div></DropdownMenuContent
                ></DropdownMenu>
                <Button
                  v-if="up(preview.status) && safeURL(preview.url)"
                  as-child
                  size="sm"
                  variant="ghost"
                  ><a
                    :href="safeURL(preview.url)"
                    target="_blank"
                    rel="noopener"
                    :aria-label="`Open ${preview.name}`"
                    ><ExternalLink :size="16" /></a></Button
                ><CopyButton
                  v-if="up(preview.status)"
                  :text="preview.url"
                  label="Copy preview URL"
                /><Button
                  size="sm"
                  variant="ghost"
                  @click="showLogs({ preview: preview.name }, preview.name)"
                  >Logs</Button
                >
                <DropdownMenu v-if="state.session.signed_in"
                  ><DropdownMenuTrigger as-child
                    ><Button variant="outline" size="sm" :aria-label="`Actions for ${preview.name}`"
                      ><MoreHorizontal :size="16" />Actions</Button
                    ></DropdownMenuTrigger
                  ><DropdownMenuContent align="end"
                    > <DropdownMenuItem :disabled="!secretsCheckout(preview)"
                      :title="!secretsCheckout(preview) ? 'The preview checkout is unavailable or ambiguous' : undefined"
                      @select="secretsPreview = preview">Secrets</DropdownMenuItem
                    ><DropdownMenuItem
                      @select="
                        act({
                          action: up(preview.status) ? 'stop' : 'start',
                          preview: preview.name,
                        })
                      "
                      >{{ up(preview.status) ? 'Stop' : 'Start' }}</DropdownMenuItem
                    ><DropdownMenuItem
                      v-if="state.session.share && up(preview.status) && !preview.share"
                      @select="act({ action: 'share', preview: preview.name })"
                      >Share publicly…</DropdownMenuItem
                    ><DropdownMenuItem
                      class="text-destructive"
                      @select="act({ action: 'destroy', preview: preview.name })"
                      >Destroy…</DropdownMenuItem
                    ></DropdownMenuContent
                  ></DropdownMenu
                >
              </div></TableCell
            ></TableRow
          >
          <TableRow v-if="expanded.has(preview.name)" class="detail-row"
            ><TableCell colspan="5"
              ><Table class="service-table"
                ><TableHeader
                  ><TableRow
                    ><TableHead>Service</TableHead><TableHead>Status</TableHead
                    ><TableHead>Ports / routes</TableHead><TableHead>Image / ID</TableHead
                    ><TableHead>Uptime</TableHead><TableHead>Inspect</TableHead></TableRow
                  ></TableHeader
                ><TableBody
                  ><TableRow v-for="service in preview.services" :key="service.name"
                    ><TableCell
                      ><div class="resource-identity">
                        <ContainerIcon :size="15" /><strong>{{ service.name }}</strong
                        ><Badge v-if="service.web" variant="outline">web</Badge>
                      </div></TableCell
                    ><TableCell data-label="Status"><StatusBadge :status="service.status" /></TableCell
                    ><TableCell data-label="Ports / routes"
                      ><div v-for="port in service.ports" :key="`${port.port}/${port.protocol}`">
                        <code
                          >{{ port.port
                          }}{{ port.protocol !== 'tcp' ? `/${port.protocol}` : '' }}</code
                        ><template v-if="port.routed">
                          ←
                          <template v-for="url in service.urls" :key="url"
                            ><span
                              v-if="url.replace(/^https?:\/\//, '').startsWith('*')"
                              :title="url"
                              >{{ routeLabel(url, preview) }} </span
                            ><a
                              v-else
                              :href="safeURL(url)"
                              target="_blank"
                              rel="noopener"
                              class="text-link"
                              >{{ routeLabel(url, preview) }}
                            </a></template
                          ></template
                        >
                      </div></TableCell
                    ><TableCell data-label="Image / ID"
                      ><p class="path table-path">{{ service.image }}</p>
                      <code>{{ service.id }}</code></TableCell
                    ><TableCell data-label="Uptime">{{ ago(service.started) }}</TableCell
                    ><TableCell
                      ><div class="actions">
                        <Button
                          variant="ghost"
                          size="sm"
                          @click="
                            showDetails(
                              { preview: preview.name, service: service.name },
                              `${preview.name} · ${service.name}`
                            )
                          "
                          >Details</Button
                        ><Button
                          variant="ghost"
                          size="sm"
                          @click="
                            showLogs(
                              { preview: preview.name, service: service.name },
                              `${preview.name} · ${service.name}`
                            )
                          "
                          >Logs</Button
                        >
                      </div></TableCell
                    ></TableRow
                  ></TableBody
                ></Table
              >
              <div v-if="recent(preview.name).length" class="recent-events">
                <p
                  v-for="(event, i) in recent(preview.name).slice(0, 4)"
                  :key="i"
                  :class="{ 'text-destructive': !event.ok }"
                >
                  <time>{{ ago(event.time) }} ago</time><strong>{{ historyVerb(event) }}</strong
                  ><span>{{ event.detail }} · {{ event.by }}</span>
                </p>
                <Button size="sm" variant="link" @click="allEvents(preview.name)"
                  >All {{ recent(preview.name).length }} events</Button
                >
              </div></TableCell
            ></TableRow
          ></template
        ></TableBody
      ></Table
    >
    <div v-if="!rows.length" class="empty-state">
      <ContainerIcon :size="28" />
      <h2>{{ state.overview ? 'No previews found' : 'Loading previews…' }}</h2>
      <p>
        {{
          filters.previews || filters.mode || filters.status
            ? 'Try changing the filters.'
            : 'Run devkit live in a worktree to start a preview.'
        }}
      </p>
    </div>
  </div>
  <Dialog :open="Boolean(secretsPreview)" @update:open="!$event && (secretsPreview = undefined)">
    <DialogContent class="preview-secrets-dialog">
      <DialogHeader>
        <DialogTitle>Preview secrets</DialogTitle>
        <DialogDescription>{{ secretsPreview?.name }} · Values stay hidden until you reveal or copy them.</DialogDescription>
      </DialogHeader>
      <ProjectSecrets v-if="secretsPreview && selectedSecretsCheckout && state.session.signed_in"
        :key="selectedSecretsCheckout.path" :root="selectedSecretsCheckout.path"
        :checkouts="[selectedSecretsCheckout]" default-scope="worktree" />
    </DialogContent>
  </Dialog>
</template>

<style scoped>
:global(.preview-secrets-dialog) {
  width: calc(100vw - 2rem);
  max-width: 72rem;
  max-height: calc(100dvh - 2rem);
  overflow-y: auto;
}
:global(.preview-secrets-dialog [data-slot="dialog-description"]) { overflow-wrap: anywhere; }

.preview-name {
  min-width: 0;
  white-space: normal;
  overflow-wrap: anywhere;
  text-align: left;
  cursor: pointer;
}
.preview-name:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 3px;
  border-radius: 2px;
}
@media (min-width: 768px) {
  .previews-inventory :deep(table) {
    table-layout: fixed;
    width: 100%;
  }
  .previews-inventory :deep(td) {
    min-width: 0;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .previews-inventory :deep(.preview-table > thead > tr > th:nth-child(1)) { width: 28%; }
  .previews-inventory :deep(.preview-table > thead > tr > th:nth-child(2)) { width: 22%; }
  .previews-inventory :deep(.preview-table > thead > tr > th:nth-child(3)) { width: 23%; }
  .previews-inventory :deep(.preview-table > thead > tr > th:nth-child(4)) { width: 8%; }
  .previews-inventory :deep(.preview-table > thead > tr > th:nth-child(5)) { width: 19%; }
  .preview-identity .resource-identity {
    display: grid;
    grid-template-columns: auto auto minmax(0, 1fr);
  }
  .previews-inventory .table-path {
    max-width: 100%;
    white-space: normal;
    overflow-wrap: anywhere;
  }
}

/* Desktop keeps the shared inventory table. Phones use the same controls in stacked rows. */
@media (max-width: 767px) {
  .previews-inventory .workspace-toolbar {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .previews-inventory .search-field {
    grid-column: 1 / -1;
    min-width: 0;
  }
  .previews-inventory :deep(.filter-select) {
    width: 100%;
    min-width: 0;
  }
  .previews-inventory :deep([data-slot='table-container']) {
    overflow: visible;
    min-width: 0;
  }
  .previews-inventory :deep(table),
  .previews-inventory :deep(tbody) {
    display: block;
    width: 100%;
  }
  .previews-inventory :deep(thead) {
    display: none;
  }
  .previews-inventory :deep(td) {
    display: block;
    min-width: 0;
    padding: 0;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .previews-inventory :deep(.group-row) {
    display: block;
    padding: .75rem 1rem;
  }
  .previews-inventory :deep(.preview-row) {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: .75rem 1rem;
    padding: 1rem;
  }
  .preview-identity,
  .preview-source,
  .preview-actions {
    grid-column: 1 / -1;
  }
  .preview-identity { order: 0; }
  .preview-status { order: 1; }
  .preview-uptime { order: 2; text-align: right; }
  .preview-source { order: 3; }
  .preview-actions { order: 4; }
  .preview-identity .resource-identity {
    display: grid;
    grid-template-columns: 2.75rem auto minmax(0, 1fr);
    gap: .5rem;
  }
  .preview-identity .table-path { margin-top: .5rem; }
  .previews-inventory :deep([data-label])::before {
    content: attr(data-label);
    display: block;
    margin-bottom: .25rem;
    color: var(--muted-foreground);
    font-size: .75rem;
    font-weight: 500;
  }
  .previews-inventory .table-path {
    max-width: 100%;
    white-space: normal;
    overflow-wrap: anywhere;
    word-break: normal;
  }
  .preview-actions .actions {
    justify-content: flex-start;
    gap: .5rem;
    padding-top: .75rem;
    border-top: 1px solid var(--border);
  }
  .previews-inventory :deep(button),
  .preview-actions :deep(a) {
    min-height: 2.75rem;
    min-width: 2.75rem;
  }
  .previews-inventory :deep(.detail-row) {
    display: block;
    padding: 0 1rem 1rem;
  }
  .previews-inventory :deep(.service-table tr) {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: .75rem 1rem;
    padding: 1rem 0;
  }
  .previews-inventory :deep(.service-table td:first-child),
  .previews-inventory :deep(.service-table td[data-label='Image / ID']),
  .previews-inventory :deep(.service-table td:last-child) {
    grid-column: 1 / -1;
  }
  .previews-inventory .recent-events p {
    display: flex;
    flex-wrap: wrap;
    gap: .25rem .5rem;
  }
  .previews-inventory .recent-events p span {
    flex-basis: 100%;
    min-width: 0;
    overflow-wrap: anywhere;
  }
}
</style>
