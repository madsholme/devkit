<script setup lang="ts">
import { computed } from 'vue'
import { FolderGit2, GitBranch, Search, ArrowUpRight } from 'lucide-vue-next'
import { Input } from '@madsholme/ui/input'
import { Badge } from '@madsholme/ui/badge'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { state, filters } from '../dashboard'
import { home, wordsMatch, up } from '../format'
import { projectInventories } from '../projects'
import { projectLocation } from '../project-inventory'
import InventoryNotice from '../components/InventoryNotice.vue'
const projects = computed(() =>
  projectInventories.value.filter((item) =>
    wordsMatch(filters.projects, [
      item.project.name,
      item.project.path,
      ...item.checkouts.flatMap((tree) => [tree.path, tree.branch, tree.project]),
      ...item.containers.map((row) => row.container.name),
      ...item.vms.map((row) => row.vm.name),
    ])
  )
)
</script>
<template>
  <InventoryNotice
    :error="Boolean(state.errors.containers || state.errors.vms)"
    :text="
      [
        ...(state.projects?.warnings || []),
        ...(state.vms?.warnings || []),
        ...(state.containers?.warnings || []),
        state.errors.vms,
        state.errors.containers,
      ]
        .filter(Boolean)
        .join(' ')
    "
  />
  <div class="workspace-surface project-directory" data-inventory>
    <div class="workspace-toolbar">
      <div class="search-field">
        <Search :size="16" /><Input
          class="pl-9"
          v-model="filters.projects"
          aria-label="Filter projects"
          placeholder="Find a project, branch, container or VM…"
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
    <Table v-if="projects.length">
      <TableHeader
        ><TableRow
          ><TableHead>Project</TableHead><TableHead>Main checkout</TableHead
          ><TableHead>Worktrees</TableHead><TableHead>Containers</TableHead
          ><TableHead>Virtual machines</TableHead
          ><TableHead><span class="sr-only">Open project</span></TableHead></TableRow
        ></TableHeader
      >
      <TableBody
        ><TableRow v-for="item in projects" :key="item.project.path" class="project-directory-row">
          <TableCell
            ><RouterLink
              :to="projectLocation(item.project.path)"
              class="project-link resource-identity"
              ><FolderGit2 :size="20" /><strong>{{ item.project.name }}</strong></RouterLink
            >
            <p class="path table-path">{{ home(item.project.path) }}</p>
            <p v-if="item.project.main.note" class="note">
              {{ item.project.main.note }}
            </p></TableCell
          >
          <TableCell
            ><div class="resource-identity">
              <GitBranch :size="15" /><span>{{
                item.project.main.branch ||
                (item.project.main.git ? 'Detached HEAD' : 'No Git repository')
              }}</span>
            </div>
            <p class="note">{{ item.project.main.subject || 'No commits' }}</p>
            <Badge v-if="item.project.main.changes" variant="warning"
              >{{ item.project.main.changes }} local changes</Badge
            ></TableCell
          >
          <TableCell
            ><RouterLink
              :to="projectLocation(item.project.path, 'worktrees')"
              class="inventory-count-link"
              >{{ item.checkouts.length }}
              <span>{{ item.checkouts.length === 1 ? 'checkout' : 'checkouts' }}</span></RouterLink
            >
            <p class="note">Main + {{ item.project.worktrees.length }} linked</p></TableCell
          >
          <TableCell
            ><RouterLink
              :to="projectLocation(item.project.path, 'containers')"
              class="inventory-count-link"
              >{{ item.containers.length }} <span>containers</span></RouterLink
            >
            <p class="note">
              {{ item.containers.filter((c) => up(c.container.status)).length }} running
            </p></TableCell
          >
          <TableCell
            ><RouterLink
              :to="projectLocation(item.project.path, 'vms')"
              class="inventory-count-link"
              >{{ item.vms.length }} <span>VMs</span></RouterLink
            >
            <p class="note">
              {{ item.vms.filter((v) => up(v.vm.state)).length }} running
            </p></TableCell
          >
          <TableCell
            ><RouterLink
              :to="projectLocation(item.project.path)"
              :aria-label="`Open ${item.project.name}`"
              class="directory-open"
              ><ArrowUpRight :size="18" /></RouterLink
          ></TableCell> </TableRow
      ></TableBody>
    </Table>
    <div v-else class="empty-state">
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
  <p class="note below-note">
    Open a project to explore its worktrees and runtimes. Resources without a unique project mapping
    remain in the global inventories.
  </p>
</template>
