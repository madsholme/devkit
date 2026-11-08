<script setup lang="ts">
import { GitBranch, ExternalLink } from 'lucide-vue-next'
import { Button } from '@madsholme/ui/button'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { state } from '../dashboard'
import { home, safeURL, up } from '../format'
import GitSummary from '../components/GitSummary.vue'
import ActionButton from '../components/ActionButton.vue'
const preview = (name: string) => state.overview?.previews.find((p) => p.name === name)
</script>
<template>
  <div class="workspace-surface" data-inventory>
    <Table
      ><TableHeader
        ><TableRow
          ><TableHead>Checkout</TableHead><TableHead>Latest commit</TableHead
          ><TableHead>Previews</TableHead><TableHead>Actions</TableHead></TableRow
        ></TableHeader
      ><TableBody
        ><template v-for="repo in state.overview?.repositories || []" :key="repo.path"
          ><TableRow class="group-row"
            ><TableCell colspan="4">{{
              home(repo.path.replace(/\/\.git$/, ''))
            }}</TableCell></TableRow
          ><TableRow v-for="tree in repo.worktrees" :key="tree.path"
            ><TableCell
              ><div class="resource-identity">
                <GitBranch :size="16" /><strong>{{ tree.branch || 'Detached' }}</strong>
              </div>
              <p class="path table-path">{{ home(tree.path) }}</p></TableCell
            ><TableCell><GitSummary :tree="tree" /></TableCell
            ><TableCell
              ><template v-for="name in tree.previews" :key="name"
                ><Button
                  v-if="preview(name) && up(preview(name)!.status) && safeURL(preview(name)!.url)"
                  as-child
                  size="sm"
                  variant="link"
                  ><a :href="safeURL(preview(name)!.url)" target="_blank" rel="noopener"
                    >{{ name }}<ExternalLink :size="14" /></a
                ></Button>
                <p v-else>
                  {{ name }} <span class="muted">{{ preview(name)?.status }}</span>
                </p></template
              ><span v-if="!tree.previews.length" class="muted">No preview</span></TableCell
            ><TableCell
              ><div class="actions">
                <ActionButton
                  v-if="!tree.previews.some((name) => preview(name)?.mode === 'live')"
                  :request="{ action: 'live', worktree: tree.path }"
                /><ActionButton
                  :request="{ action: 'review', worktree: tree.path }"
                /></div></TableCell></TableRow></template></TableBody
    ></Table>
    <div v-if="!state.overview?.repositories.length" class="empty-state">
      <GitBranch :size="28" />
      <h2>{{ state.overview ? 'No repositories yet' : 'Loading worktrees…' }}</h2>
      <p>Repositories appear once they have a preview or a harness worktree.</p>
    </div>
  </div>
</template>
