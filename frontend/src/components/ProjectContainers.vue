<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Container as ContainerIcon, Search } from 'lucide-vue-next'
import { Input } from '@madsholme/ui/input'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import type { ScopedContainer } from '../project-inventory'
import { wordsMatch, short } from '../format'
import ContainerControls from './ContainerControls.vue'
import StatusBadge from './StatusBadge.vue'
import WorktreeAssociation from './WorktreeAssociation.vue'
const props = defineProps<{ rows: ScopedContainer[]; project: string; scope: string }>()
const search = ref('')
watch(
  () => props.scope,
  () => {
    search.value = ''
  }
)
const filtered = computed(() =>
  props.rows.filter(({ container: c, checkout: t }) =>
    wordsMatch(search.value, [c.name, c.image, c.service, c.preview, c.status, t?.path, t?.branch])
  )
)
</script>
<template>
  <div class="workspace-surface" data-inventory>
    <div class="workspace-toolbar">
      <div class="search-field">
        <Search :size="16" /><Input
          v-model="search"
          class="pl-9"
          aria-label="Filter project containers"
          placeholder="Filter containers, worktrees or branches…"
        />
      </div>
      <span class="result-count"
        >{{ filtered.length }} {{ filtered.length === 1 ? 'container' : 'containers' }}</span
      >
    </div>
    <Table v-if="filtered.length"
      ><TableHeader
        ><TableRow
          ><TableHead>Container</TableHead><TableHead>Status</TableHead
          ><TableHead>Worktree / Git branch</TableHead><TableHead>Preview / service</TableHead
          ><TableHead class="text-right">Actions</TableHead></TableRow
        ></TableHeader
      >
      <TableBody
        ><TableRow v-for="row in filtered" :key="row.container.id">
          <TableCell
            ><div class="resource-identity">
              <ContainerIcon :size="17" /><strong>{{ row.container.name }}</strong>
            </div>
            <p class="path table-path">{{ row.container.image }}</p>
            <p v-for="port in row.container.ports" :key="port" class="note">
              {{ port }}
            </p></TableCell
          >
          <TableCell
            ><StatusBadge :status="row.container.status" />
            <p class="path" :title="row.container.id">{{ short(row.container.id) }}</p></TableCell
          >
          <TableCell
            ><WorktreeAssociation :project="project" :checkout="row.checkout" :note="row.note"
          /></TableCell>
          <TableCell
            >{{ row.container.preview || 'Registered stack' }}
            <p class="note">{{ row.container.service || 'No service label' }}</p></TableCell
          >
          <TableCell
            ><ContainerControls :container="row.container"
          /></TableCell> </TableRow></TableBody
    ></Table>
    <div v-else class="empty-state">
      <ContainerIcon :size="26" />
      <h3>{{ search ? 'No matching containers' : 'No containers in this scope' }}</h3>
      <p>
        {{
          search
            ? 'Try a container name or branch.'
            : 'Launch a configured worktree or register its existing Compose stack.'
        }}
      </p>
    </div>
  </div>
</template>
