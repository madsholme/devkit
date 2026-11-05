<script setup lang="ts">
import { computed, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronRight, FolderGit2, Monitor, GitBranch, Search } from 'lucide-vue-next'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@madsholme/ui/collapsible'
import { Input } from '@madsholme/ui/input'
import { Button } from '@madsholme/ui/button'
import { Badge } from '@madsholme/ui/badge'
import { state, filters } from '../dashboard'
import { home, wordsMatch } from '../format'
import CheckoutRow from '../components/CheckoutRow.vue'
import VMControls from '../components/VMControls.vue'
import StatusBadge from '../components/StatusBadge.vue'
import InventoryNotice from '../components/InventoryNotice.vue'
const router = useRouter()
const closed = reactive(new Set<string>())
const projects = computed(() =>
  (state.projects?.projects || [])
    .map((project) => ({
      ...project,
      vms: (state.vms?.vms || []).filter(
        (vm) =>
          vm.project &&
          [project.main, ...project.worktrees].some((tree) => tree.project === vm.project)
      ),
    }))
    .filter((project) =>
      wordsMatch(filters.projects, [
        project.name,
        project.path,
        ...[project.main, ...project.worktrees].flatMap((tree) => [
          tree.path,
          tree.branch,
          tree.project,
          ...(tree.containers || []).flatMap((c) => [c.name, c.service]),
        ]),
        ...project.vms.map((vm) => vm.name),
      ])
    )
)
function viewVM(name: string) {
  filters.vms = name
  void router.push('/vms')
}
</script>
<template>
  <InventoryNotice
    :text="
      [...(state.projects?.warnings || []), ...(state.vms?.warnings || []), state.errors.vms]
        .filter(Boolean)
        .join(' ')
    "
  />
  <div class="workspace-surface" data-inventory>
    <div class="workspace-toolbar">
      <div class="search-field">
        <Search :size="16" /><Input
          class="pl-9"
          v-model="filters.projects"
          aria-label="Filter projects"
          placeholder="Filter projects, branches or VMs…"
          @keydown.esc="filters.projects = ''"
        />
      </div>
      <span class="result-count">{{ projects.length }} projects</span>
    </div>
    <p class="directory-note">
      {{
        state.projects?.directory
          ? `Projects in ${home(state.projects.directory)}`
          : 'Set projects_dir in the host configuration to discover project folders.'
      }}
    </p>
    <Collapsible
      v-for="project in projects"
      :key="project.path"
      :open="!closed.has(project.path)"
      @update:open="$event ? closed.delete(project.path) : closed.add(project.path)"
      class="project-group"
    >
      <CollapsibleTrigger class="project-trigger"
        ><ChevronRight :size="16" :class="{ rotated: !closed.has(project.path) }" /><FolderGit2
          :size="20"
        /><span class="project-name"
          >{{ project.name }}<span class="path">{{ home(project.path) }}</span></span
        ><span class="project-counts"
          ><Badge variant="secondary"
            >{{ project.worktrees.length }}
            {{ project.worktrees.length === 1 ? 'worktree' : 'worktrees' }}</Badge
          ><Badge v-if="project.vms.length" variant="outline"
            >{{ project.vms.length }} {{ project.vms.length === 1 ? 'VM' : 'VMs' }}</Badge
          ></span
        ></CollapsibleTrigger
      >
      <CollapsibleContent class="project-content"
        ><CheckoutRow
          :tree="project.main"
          :label="project.main.git ? 'Main checkout' : 'Project folder'"
        />
        <template v-if="project.vms.length"
          ><h3 class="subsection-heading"><Monitor :size="16" />Virtual machines</h3>
          <div v-for="vm in project.vms" :key="vm.id" class="resource-line project-vm">
            <div class="resource-identity">
              <Monitor :size="16" /><strong>{{ vm.name }}</strong
              ><StatusBadge :status="vm.state" />
            </div>
            <div class="actions">
              <VMControls :vm="vm" /><Button variant="ghost" size="sm" @click="viewVM(vm.name)"
                >Details &amp; connect</Button
              >
            </div>
            <p v-if="vm.control_note" class="note w-full">{{ vm.control_note }}</p>
          </div></template
        >
        <template v-if="project.worktrees.length"
          ><h3 class="subsection-heading"><GitBranch :size="16" />Worktrees</h3>
          <CheckoutRow
            v-for="tree in project.worktrees"
            :key="tree.path"
            :tree="tree"
            :label="tree.branch || 'Detached worktree'"
        /></template>
      </CollapsibleContent>
    </Collapsible>
    <div v-if="!projects.length" class="empty-state">
      <FolderGit2 :size="28" />
      <h2>
        {{
          state.projects
            ? filters.projects
              ? 'No matching projects'
              : 'No projects discovered'
            : state.errors.projects
            ? 'Project discovery unavailable'
            : 'Loading projects…'
        }}
      </h2>
      <p>
        {{
          filters.projects
            ? 'Try another name, path or branch.'
            : 'Configure projects_dir on the host, then refresh.'
        }}
      </p>
    </div>
  </div>
</template>
