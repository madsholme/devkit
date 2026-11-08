<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Monitor, Search } from 'lucide-vue-next'
import { Input } from '@madsholme/ui/input'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import type { ScopedVM } from '../project-inventory'
import { bytes, wordsMatch } from '../format'
import VMControls from './VMControls.vue'
import VMConnections from './VMConnections.vue'
import StatusBadge from './StatusBadge.vue'
import WorktreeAssociation from './WorktreeAssociation.vue'
const props = defineProps<{ rows: ScopedVM[]; project: string; scope: string }>()
const search = ref('')
watch(
  () => props.scope,
  () => {
    search.value = ''
  }
)
const filtered = computed(() =>
  props.rows.filter(({ vm, checkout }) =>
    wordsMatch(search.value, [vm.name, vm.source, vm.state, checkout?.path, checkout?.branch])
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
          aria-label="Filter project virtual machines"
          placeholder="Filter VMs, worktrees or branches…"
        />
      </div>
      <span class="result-count"
        >{{ filtered.length }}
        {{ filtered.length === 1 ? 'virtual machine' : 'virtual machines' }}</span
      >
    </div>
    <Table v-if="filtered.length"
      ><TableHeader
        ><TableRow
          ><TableHead>Virtual machine</TableHead><TableHead>Worktree / Git branch</TableHead
          ><TableHead>Allocated resources</TableHead><TableHead>Host usage</TableHead
          ><TableHead class="text-right">Actions</TableHead></TableRow
        ></TableHeader
      >
      <TableBody
        ><template v-for="row in filtered" :key="row.vm.id"
          ><TableRow>
            <TableCell
              ><div class="resource-identity">
                <Monitor :size="18" /><strong>{{ row.vm.name }}</strong>
              </div>
              <div class="metadata">
                <StatusBadge :status="row.vm.state" /><span>{{
                  row.vm.display.toUpperCase()
                }}</span>
              </div>
              <p class="note">
                {{ row.vm.source === 'qemu' ? 'Standalone QEMU' : row.vm.source }}
              </p></TableCell
            >
            <TableCell
              ><WorktreeAssociation :project="project" :checkout="row.checkout" :note="row.note"
            /></TableCell>
            <TableCell
              >{{ row.vm.vcpus || 'Unknown' }} vCPUs · {{ bytes(row.vm.memory_bytes) }} RAM
              <p class="note">Disk: {{ bytes(row.vm.disk_bytes) }}</p>
              <p class="note">{{ row.vm.graphics.join(', ') }}</p></TableCell
            >
            <TableCell
              >{{
                row.vm.cpu_percent == null
                  ? 'CPU sample unavailable'
                  : `${row.vm.cpu_percent.toFixed(1)}% CPU`
              }}
              <p class="note">{{ bytes(row.vm.rss_bytes) }} resident</p></TableCell
            >
            <TableCell><VMControls :vm="row.vm" /></TableCell> </TableRow
          ><TableRow
            v-if="row.vm.connections.length || row.vm.control_note || row.vm.connection_note"
            class="detail-row"
            ><TableCell colspan="5"
              ><VMConnections :vm="row.vm" />
              <p
                v-for="note in [row.vm.control_note, row.vm.connection_note].filter(Boolean)"
                :key="note"
                class="note"
              >
                {{ note }}
              </p></TableCell
            ></TableRow
          ></template
        ></TableBody
      ></Table
    >
    <div v-else class="empty-state">
      <Monitor :size="26" />
      <h3>{{ search ? 'No matching virtual machines' : 'No virtual machines in this scope' }}</h3>
      <p>
        {{
          search
            ? 'Try a VM name or branch.'
            : 'VMs appear here when a project recipe maps them. Shared mappings are shown in the project’s Virtual machines tab.'
        }}
      </p>
    </div>
  </div>
</template>
