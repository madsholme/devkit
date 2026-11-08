<script setup lang="ts">
import { computed } from 'vue'
import { Container as ContainerIcon, Search } from 'lucide-vue-next'
import { Input } from '@madsholme/ui/input'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { filters, state } from '../dashboard'
import { short, up, wordsMatch } from '../format'
import ContainerControls from '../components/ContainerControls.vue'
import StatusBadge from '../components/StatusBadge.vue'
import FilterSelect from '../components/FilterSelect.vue'
import InventoryNotice from '../components/InventoryNotice.vue'
const rows = computed(() =>
  (state.containers?.containers || []).filter(
    (c) =>
      wordsMatch(filters.containers, [c.name, c.project, c.preview, c.image, c.id, c.status]) &&
      (!filters.containerStatus || up(c.status) === (filters.containerStatus === 'up'))
  )
)
</script>
<template>
  <InventoryNotice :text="state.containers?.warnings?.join(' ')" />
  <div class="workspace-surface" data-inventory>
    <div class="workspace-toolbar">
      <div class="search-field">
        <Search :size="16" /><Input
          class="pl-9"
          v-model="filters.containers"
          aria-label="Filter containers"
          placeholder="Filter containers, projects or images…"
          @keydown.esc="filters.containers = ''"
        />
      </div>
      <FilterSelect
        v-model="filters.containerStatus"
        label="Container status"
        :options="[
          { value: '', label: 'Any status' },
          { value: 'up', label: 'Running' },
          { value: 'down', label: 'Not running' },
        ]"
      /><span class="result-count">{{ rows.length }} containers</span>
    </div>
    <Table
      ><TableHeader
        ><TableRow
          ><TableHead>Container</TableHead><TableHead>Status</TableHead
          ><TableHead>Project</TableHead><TableHead>Host → container ports</TableHead
          ><TableHead class="text-right">Actions</TableHead></TableRow
        ></TableHeader
      ><TableBody
        ><TableRow v-for="container in rows" :key="container.id"
          ><TableCell
            ><div class="resource-identity">
              <ContainerIcon :size="16" /><strong>{{ container.name }}</strong>
            </div>
            <p class="path table-path">{{ container.image }}</p></TableCell
          ><TableCell
            ><StatusBadge :status="container.status" />
            <p class="path" :title="container.id">{{ short(container.id) }}</p></TableCell
          ><TableCell
            ><strong>{{ container.project || 'Unassigned' }}</strong>
            <p class="note">
              {{
                container.preview ||
                (container.registered
                  ? 'Registered existing stack'
                  : container.managed
                  ? 'Managed by devkit'
                  : 'Outside devkit')
              }}
            </p></TableCell
          ><TableCell
            ><code v-for="port in container.ports" :key="port" class="block">{{ port }}</code
            ><span v-if="!container.ports.length" class="muted">No published ports</span></TableCell
          ><TableCell><ContainerControls :container="container" /></TableCell></TableRow></TableBody
    ></Table>
    <div v-if="!rows.length" class="empty-state">
      <ContainerIcon :size="28" />
      <h2>
        {{
          state.containers
            ? 'No containers found'
            : state.errors.containers
            ? 'Container inventory unavailable'
            : 'Loading containers…'
        }}
      </h2>
      <p>
        {{
          filters.containers || filters.containerStatus
            ? 'Try changing the filters.'
            : 'Launch a configured project from Projects to create a preview.'
        }}
      </p>
    </div>
  </div>
  <p class="note below-note">
    Port mappings show configured bindings and may be inactive while a container is stopped.
  </p>
</template>
