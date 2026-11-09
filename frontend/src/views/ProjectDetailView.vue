<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import {
  ArrowLeft,
  FolderGit2,
  GitBranch,
  Container,
  Monitor,
  Info,
  BookOpen,
  KeyRound,
  RefreshCw,
  ChevronRight,
} from 'lucide-vue-next'
import { Button } from '@madsholme/ui/button'
import { Badge } from '@madsholme/ui/badge'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { state, refresh } from '../dashboard'
import { projectInventories } from '../projects'
import { projectLocation } from '../project-inventory'
import { home, short, up } from '../format'
import CheckoutRow from '../components/CheckoutRow.vue'
import ProjectContainers from '../components/ProjectContainers.vue'
const ProjectDocuments = defineAsyncComponent(() => import('../components/ProjectDocuments.vue'))
import ProjectSecrets from '../components/ProjectSecrets.vue'
import ProjectVMs from '../components/ProjectVMs.vue'
import InventoryNotice from '../components/InventoryNotice.vue'
import type { Checkout } from '../types'
import { request } from '../api'
import { secretRequest, type SecretReply } from '../secrets'
const route = useRoute()
const inventory = computed(() =>
  projectInventories.value.find((i) => i.project.path === route.params.project)
)
const project = computed(() => inventory.value?.project)
const sections = [
  { id: 'overview', label: 'Overview', icon: Info },
  { id: 'documents', label: 'Documents', icon: BookOpen },
  { id: 'worktrees', label: 'Worktrees', icon: GitBranch },
  { id: 'secrets', label: 'Secrets', icon: KeyRound },
  { id: 'containers', label: 'Containers', icon: Container },
  { id: 'vms', label: 'Virtual machines', icon: Monitor },
]
const section = computed(() => String(route.params.section || 'overview'))
const knownSection = computed(() => sections.some((s) => s.id === section.value))
const treePath = computed(() => (typeof route.query.tree === 'string' ? route.query.tree : ''))
const tree = computed(() => inventory.value?.checkouts.find((t) => t.path === treePath.value))
const resourceSections = [
  { id: 'overview', label: 'Information', icon: Info },
  { id: 'secrets', label: 'Secrets', icon: KeyRound },
  { id: 'containers', label: 'Containers', icon: Container },
  { id: 'vms', label: 'Virtual machines', icon: Monitor },
]
const resource = computed(() => String(route.query.resource || 'overview'))
const knownResource = computed(() => resourceSections.some((s) => s.id === resource.value))
const busy = computed(
  () => state.busy.projects || state.busy.containers || state.busy.vms || state.busy.overview
)
const containersFor = (t: Checkout) =>
  inventory.value?.containers.filter((r) => r.checkout?.path === t.path) || []
const vmsFor = (t: Checkout) =>
  inventory.value?.vms.filter((r) => r.checkout?.path === t.path) || []
const secretSummary = ref<SecretReply>()
const documentCount = ref<number>()
let secretGeneration = 0, documentGeneration = 0
const secretTotal = computed(() => secretSummary.value?.counts ? Object.values(secretSummary.value.counts).reduce((sum, n) => sum + n, 0) : undefined)
const documentRoot = computed(() => section.value === 'documents' && treePath.value ? tree.value?.path : project.value?.main.path)
function updateSecretSummary(reply: SecretReply) {
  if (state.session.signed_in && inventory.value?.checkouts.some(t => t.path === reply.context.root)) { secretGeneration++; secretSummary.value = reply }
}
function updateDocumentCount(count: number, root: string) {
  if (root === documentRoot.value) { documentGeneration++; documentCount.value = count }
}
function worktreeSecretCount(path: string) {
  const owner = secretSummary.value?.resources?.find(r => r.kind === 'worktree' && r.path === path && r.state === 'active')
  return owner ? secretSummary.value?.counts?.[owner.id] : undefined
}
watch(() => [project.value?.main.path, state.session.signed_in, state.updated.projects], async () => {
  const current = ++secretGeneration
  secretSummary.value = undefined
  const root = project.value?.main.path
  if (!root || !state.session.signed_in) return
  try {
    const reply = await secretRequest({ operation: 'context', root })
    if (current === secretGeneration) updateSecretSummary(reply)
  } catch { /* An unavailable or unenrolled context is not a zero count. */ }
}, { immediate: true })
watch(() => [documentRoot.value, state.updated.projects], async () => {
  const current = ++documentGeneration
  documentCount.value = undefined
  const root = documentRoot.value
  if (!root) return
  try {
    const reply = await request<{ files: unknown[] }>(`documents?${new URLSearchParams({ worktree: root })}`)
    if (current === documentGeneration) documentCount.value = reply.files.length
  } catch { /* Keep unavailable distinct from an empty document list. */ }
}, { immediate: true })
onBeforeUnmount(() => { secretGeneration++; documentGeneration++ })
const summary = computed(() =>
  inventory.value
    ? [
        {
          id: 'worktrees',
          label: 'Worktrees',
          value: inventory.value.checkouts.length,
          detail: `Main checkout + ${project.value!.worktrees.length} linked`,
          icon: GitBranch,
        },
        {
          id: 'containers',
          label: 'Containers',
          value: inventory.value.containers.length,
          detail: `${
            inventory.value.containers.filter((r) => up(r.container.status)).length
          } running`,
          icon: Container,
        },
        {
          id: 'vms',
          label: 'Virtual machines',
          value: inventory.value.vms.length,
          detail: `${inventory.value.vms.filter((r) => up(r.vm.state)).length} running`,
          icon: Monitor,
        },
      ]
    : []
)
</script>
<template>
  <section class="project-detail">
    <RouterLink to="/projects" class="back-link"
      ><ArrowLeft :size="15" />Back to Projects</RouterLink
    >
    <template v-if="project && inventory">
      <header class="workspace-heading project-heading">
        <div>
          <h1><FolderGit2 :size="26" />{{ project.name }}</h1>
          <p class="path">{{ home(project.path) }}</p>
        </div>
        <div class="heading-actions">
          <Badge variant="outline">{{
            project.main.git ? 'Git repository' : 'Project folder'
          }}</Badge
          ><span v-if="state.updated.projects" class="updated"
            >Updated {{ state.updated.projects }}</span
          ><Button variant="outline" :disabled="busy" @click="refresh"
            ><RefreshCw :size="15" :class="{ 'animate-spin': busy }" />Refresh</Button
          >
        </div>
      </header>
      <InventoryNotice
        :error="Boolean(state.errors.containers || state.errors.vms)"
        :text="
          [
            ...(state.projects?.warnings || []),
            ...(state.containers?.warnings || []),
            ...(state.vms?.warnings || []),
            state.errors.containers,
            state.errors.vms,
          ]
            .filter(Boolean)
            .join(' ')
        "
      />
      <nav class="project-section-nav" aria-label="Project sections">
        <RouterLink
          v-for="item in sections"
          :key="item.id"
          :to="projectLocation(project.path, item.id)"
          :aria-current="section === item.id ? 'page' : undefined"
          ><component :is="item.icon" :size="16" />{{ item.label
          }}<span v-if="item.id === 'documents'" class="section-count" :title="documentCount === undefined ? 'Document count unavailable' : 'Documents in the selected checkout'">{{ documentCount ?? '—' }}</span><span v-if="item.id === 'secrets' && state.session.signed_in" class="section-count" :title="secretTotal === undefined ? 'Secret count unavailable; open Secrets to check registration' : 'Active saved entries across all project scopes'">{{ secretTotal ?? '—' }}</span><span v-if="item.id === 'worktrees'" class="section-count">{{
            inventory.checkouts.length
          }}</span
          ><span v-if="item.id === 'containers'" class="section-count">{{
            inventory.containers.length
          }}</span
          ><span v-if="item.id === 'vms'" class="section-count">{{
            inventory.vms.length
          }}</span></RouterLink
        >
      </nav>
      <div v-if="!knownSection" class="empty-state">
        <h2>Project section not found</h2>
        <RouterLink :to="projectLocation(project.path)" class="project-link"
          >Return to overview</RouterLink
        >
      </div>
      <template v-else-if="section === 'overview'">
        <h2 class="sr-only">Project overview</h2>
        <div class="project-summary-strip">
          <RouterLink
            v-for="item in summary"
            :key="item.id"
            :to="projectLocation(project.path, item.id)"
            ><component :is="item.icon" :size="22" /><span
              ><span class="summary-strip-label">{{ item.label }}</span
              ><strong
                >{{ item.value }} <span>{{ item.detail }}</span></strong
              ></span
            ><ChevronRight :size="17"
          /></RouterLink>
        </div>
        <div class="project-overview-grid">
          <section class="workspace-surface">
            <div class="panel-heading">
              <Info :size="18" />
              <h2>Project information</h2>
            </div>
            <dl class="project-facts">
              <div>
                <dt>Location</dt>
                <dd class="path">{{ home(project.path) }}</dd>
              </div>
              <div>
                <dt>Recipe project</dt>
                <dd>{{ project.main.project || 'Not configured' }}</dd>
              </div>
              <div>
                <dt>Main branch</dt>
                <dd>
                  {{
                    project.main.branch ||
                    (project.main.git ? 'Detached HEAD' : 'No Git repository')
                  }}
                </dd>
              </div>
              <div>
                <dt>Upstream</dt>
                <dd>{{ project.main.upstream || 'Not configured' }}</dd>
              </div>
              <div>
                <dt>Container previews</dt>
                <dd>
                  {{ project.main.docker ? 'Configured' : 'Not configured for the main checkout' }}
                </dd>
              </div>
              <div>
                <dt>VM mapping</dt>
                <dd>
                  {{
                    inventory.vms.length
                      ? `${
                          inventory.vms.filter((v) => v.checkout).length
                        } with an exact worktree · ${
                          inventory.vms.filter((v) => !v.checkout).length
                        } project-level`
                      : 'No mapped VMs discovered'
                  }}
                </dd>
              </div>
            </dl>
          </section>
          <section class="workspace-surface">
            <div class="panel-heading">
              <GitBranch :size="18" />
              <h2>Main checkout</h2>
              <RouterLink
                :to="projectLocation(project.path, 'worktrees', project.main.path)"
                class="project-link"
                >Open worktree<ChevronRight :size="15"
              /></RouterLink>
            </div>
            <CheckoutRow :tree="project.main" label="Main checkout" :show-containers="false" />
          </section>
        </div>
      </template>
      <template v-else-if="section === 'worktrees'">
        <template v-if="treePath">
          <RouterLink
            :to="projectLocation(project.path, 'worktrees')"
            class="back-link worktree-back"
            ><ArrowLeft :size="14" />All worktrees</RouterLink
          >
          <template v-if="tree">
            <header class="worktree-heading">
              <div>
                <h2>
                  <GitBranch :size="21" />{{
                    tree.branch || (tree.git ? 'Detached HEAD' : 'Project folder')
                  }}<Badge v-if="tree.path === project.main.path" variant="secondary"
                    >Main checkout</Badge
                  >
                </h2>
                <p class="path">{{ home(tree.path) }}</p>
              </div>
              <Badge v-if="tree.changes" variant="warning">{{ tree.changes }} local changes</Badge>
            </header>
            <nav class="project-section-nav worktree-section-nav" aria-label="Worktree sections">
              <RouterLink
                v-for="item in resourceSections"
                :key="item.id"
                :to="projectLocation(project.path, 'worktrees', tree.path, item.id)"
                :aria-current="resource === item.id ? 'page' : undefined"
                ><component :is="item.icon" :size="15" />{{ item.label
                }}<span v-if="item.id === 'secrets' && state.session.signed_in" class="section-count" title="Active entries saved in this worktree">{{ worktreeSecretCount(tree.path) ?? '—' }}</span><span v-if="item.id === 'containers'" class="section-count">{{
                  containersFor(tree).length
                }}</span
                ><span v-if="item.id === 'vms'" class="section-count">{{
                  vmsFor(tree).length
                }}</span></RouterLink
              >
            </nav>
            <div v-if="!knownResource" class="empty-state">
              <h3>Worktree section not found</h3>
              <RouterLink
                :to="projectLocation(project.path, 'worktrees', tree.path)"
                class="project-link"
                >Return to information</RouterLink
              >
            </div>
            <section v-else-if="resource === 'overview'" class="workspace-surface">
              <div class="panel-heading">
                <Info :size="18" />
                <h3>Checkout information</h3>
              </div>
              <CheckoutRow
                :tree="tree"
                :label="tree.branch || 'Project folder'"
                :show-containers="false"
              />
            </section>
            <ProjectSecrets v-else-if="resource === 'secrets'" :key="tree.path" :root="tree.path" :checkouts="[tree]" default-scope="worktree" @context="updateSecretSummary" />
            <ProjectContainers
              v-else-if="resource === 'containers'"
              :rows="containersFor(tree)"
              :project="project.path"
              :scope="tree.path"
            />
            <ProjectVMs v-else :rows="vmsFor(tree)" :project="project.path" :scope="tree.path" />
          </template>
          <div v-else class="empty-state">
            <GitBranch :size="26" />
            <h2>Worktree not found</h2>
            <p>
              This checkout is no longer in the project inventory. Choose another worktree or
              refresh.
            </p>
          </div>
        </template>
        <div v-else class="workspace-surface" data-inventory>
          <div class="panel-heading">
            <GitBranch :size="18" />
            <h2>Worktrees</h2>
            <span class="note">Main and linked checkouts</span>
          </div>
          <Table
            ><TableHeader
              ><TableRow
                ><TableHead>Worktree / branch</TableHead><TableHead>Latest commit</TableHead
                ><TableHead>Working tree</TableHead><TableHead>Containers</TableHead
                ><TableHead>Virtual machines</TableHead></TableRow
              ></TableHeader
            ><TableBody
              ><TableRow v-for="checkout in inventory.checkouts" :key="checkout.path">
                <TableCell
                  ><RouterLink
                    :to="projectLocation(project.path, 'worktrees', checkout.path)"
                    class="project-link resource-identity"
                    ><GitBranch :size="17" /><strong>{{
                      checkout.branch || (checkout.git ? 'Detached HEAD' : 'Project folder')
                    }}</strong></RouterLink
                  >
                  <p class="path table-path">{{ home(checkout.path) }}</p>
                  <Badge v-if="checkout.path === project.main.path" variant="secondary"
                    >Main checkout</Badge
                  ></TableCell
                >
                <TableCell
                  >{{ checkout.subject || 'No commits' }}
                  <p class="path">{{ short(checkout.head) }}</p></TableCell
                >
                <TableCell
                  ><Badge v-if="checkout.changes" variant="warning"
                    >{{ checkout.changes }} changes</Badge
                  ><span v-else class="muted">{{
                    checkout.git ? 'Clean' : 'Not a Git checkout'
                  }}</span>
                  <p class="note">
                    {{
                      checkout.upstream
                        ? `${checkout.ahead} ahead · ${checkout.behind} behind`
                        : 'No upstream'
                    }}
                  </p></TableCell
                >
                <TableCell
                  ><RouterLink
                    :to="projectLocation(project.path, 'worktrees', checkout.path, 'containers')"
                    class="inventory-count-link"
                    >{{ containersFor(checkout).length }} containers</RouterLink
                  >
                  <p class="note">{{ checkout.previews.length }} previews</p></TableCell
                >
                <TableCell
                  ><RouterLink
                    :to="projectLocation(project.path, 'worktrees', checkout.path, 'vms')"
                    class="inventory-count-link"
                    >{{ vmsFor(checkout).length }} VMs</RouterLink
                  ></TableCell
                >
              </TableRow></TableBody
            ></Table
          >
          <p v-if="inventory.vms.some((v) => !v.checkout)" class="directory-note">
            Some VMs have no unique worktree mapping. Find them in the project’s
            <RouterLink :to="projectLocation(project.path, 'vms')" class="project-link"
              >Virtual machines</RouterLink
            >
            tab.
          </p>
        </div>
      </template>
      <ProjectSecrets v-else-if="section === 'secrets'" :key="project.path" :root="project.main.path" :checkouts="inventory.checkouts" @context="updateSecretSummary" />
      <ProjectDocuments
        v-else-if="section === 'documents'"
        :checkouts="inventory.checkouts"
        :main="project.main.path"
        @count="updateDocumentCount"
      />
      <ProjectContainers
        v-else-if="section === 'containers'"
        :rows="inventory.containers"
        :project="project.path"
        :scope="project.path"
      />
      <ProjectVMs
        v-else-if="section === 'vms'"
        :rows="inventory.vms"
        :project="project.path"
        :scope="project.path"
      />
    </template>
    <div v-else class="empty-state">
      <FolderGit2 :size="28" />
      <h1>
        {{
          state.projects
            ? 'Project not found'
            : state.errors.projects
            ? 'Project discovery unavailable'
            : 'Loading project…'
        }}
      </h1>
      <p>
        {{
          state.projects
            ? 'This project is no longer in the discovered inventory. Return to Projects or refresh.'
            : 'Reading the project inventory from the host.'
        }}
      </p>
      <Button v-if="state.projects || state.errors.projects" variant="outline" @click="refresh"
        >Refresh</Button
      >
    </div>
  </section>
</template>
