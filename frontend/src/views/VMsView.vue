<script setup lang="ts">
import { computed } from 'vue'
import { Monitor, Terminal, Search } from 'lucide-vue-next'
import { Input } from '@madsholme/ui/input'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { filters, state } from '../dashboard'
import { bytes, wordsMatch } from '../format'
import StatusBadge from '../components/StatusBadge.vue'
import VMControls from '../components/VMControls.vue'
import VMConnections from '../components/VMConnections.vue'
import InventoryNotice from '../components/InventoryNotice.vue'
const rows = computed(() =>
  (state.vms?.vms || [])
    .filter((vm) =>
      wordsMatch(filters.vms, [vm.name, vm.project || 'Other VMs', vm.source, vm.display, vm.state])
    )
    .sort((a, b) =>
      a.project === b.project
        ? a.name.localeCompare(b.name)
        : !a.project
        ? 1
        : !b.project
        ? -1
        : a.project.localeCompare(b.project)
    )
)
</script>
<template>
  <InventoryNotice :text="state.vms?.warnings.join(' ')" />
  <div class="workspace-surface" data-inventory>
    <div class="workspace-toolbar">
      <div class="search-field">
        <Search :size="16" /><Input
          class="pl-9"
          v-model="filters.vms"
          aria-label="Filter virtual machines"
          placeholder="Filter by VM or project…"
          @keydown.esc="filters.vms = ''"
        />
      </div>
      <span class="result-count">{{ rows.length }} virtual machines</span>
    </div>
    <Table
      ><TableHeader
        ><TableRow
          ><TableHead>Virtual machine</TableHead><TableHead>Allocated resources</TableHead
          ><TableHead>Graphics / GPU</TableHead><TableHead>Host usage</TableHead
          ><TableHead>Controls</TableHead></TableRow
        ></TableHeader
      ><TableBody
        ><template v-for="(vm, index) in rows" :key="vm.id"
          ><TableRow v-if="index === 0 || rows[index - 1]?.project !== vm.project" class="group-row"
            ><TableCell colspan="5">{{ vm.project || 'Other VMs' }}</TableCell></TableRow
          ><TableRow
            ><TableCell
              ><div class="resource-identity">
                <component
                  :is="vm.display === 'headless' ? Terminal : Monitor"
                  :size="18"
                /><strong>{{ vm.name }}</strong>
              </div>
              <div class="metadata">
                <StatusBadge :status="vm.state" /><span>{{
                  vm.display === 'headless'
                    ? 'Headless'
                    : vm.display === 'unknown'
                    ? 'Display unknown'
                    : vm.display.toUpperCase()
                }}</span>
              </div>
              <p class="note">
                {{ vm.source === 'qemu' ? 'Standalone QEMU' : vm.source
                }}{{ vm.pid ? ` · PID ${vm.pid}` : '' }}
              </p></TableCell
            ><TableCell
              >{{ vm.vcpus || 'Unknown' }} vCPUs ·
              {{ vm.memory_bytes ? bytes(vm.memory_bytes) : 'Unavailable' }} RAM
              <p class="note">
                Disk: {{ vm.disk_bytes ? bytes(vm.disk_bytes) : 'Unavailable' }}
              </p></TableCell
            ><TableCell>{{ vm.graphics.join(', ') || 'Not reported' }}</TableCell
            ><TableCell
              >{{
                vm.cpu_percent == null
                  ? vm.cpu_status === 'unavailable'
                    ? 'CPU unavailable'
                    : 'Awaiting CPU sample'
                  : `${vm.cpu_percent.toFixed(1)}% CPU`
              }}
              <p class="note">
                {{ vm.rss_bytes ? bytes(vm.rss_bytes) : 'Unavailable' }} resident
              </p></TableCell
            ><TableCell><VMControls :vm="vm" /></TableCell
          ></TableRow>
          <TableRow
            v-if="
              vm.connections.some((c) => c.command) ||
              vm.control_note ||
              vm.project_note ||
              vm.connection_note
            "
            class="detail-row"
            ><TableCell colspan="5"
              ><VMConnections :vm="vm" />
              <p
                v-for="note in [vm.control_note, vm.project_note, vm.connection_note].filter(
                  Boolean
                )"
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
    <div v-if="!rows.length" class="empty-state">
      <Monitor :size="28" />
      <h2>
        {{
          state.vms
            ? 'No virtual machines found'
            : state.errors.vms
            ? 'VM inventory unavailable'
            : 'Loading virtual machines…'
        }}
      </h2>
      <p>
        {{
          filters.vms
            ? 'Try a different name or display type.'
            : 'Configure VM lifecycle controls in your project recipe, or start a VM with your existing workflow, then refresh.'
        }}
      </p>
    </div>
  </div>
</template>
