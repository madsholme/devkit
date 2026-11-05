<script setup lang="ts">
import { computed } from 'vue'
import { Monitor, Terminal, Search } from 'lucide-vue-next'
import { Input } from '@madsholme/ui/input'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { filters, state } from '../dashboard'
import { bytes, wordsMatch } from '../format'
import StatusBadge from '../components/StatusBadge.vue'
import VMControls from '../components/VMControls.vue'
import CopyButton from '../components/CopyButton.vue'
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
              ><details v-if="vm.connections.some((c) => c.command)" class="connection-details">
                <summary>Connect privately</summary>
                <p class="note">
                  Run these commands on your computer. Keep the tunnel terminal open, then connect
                  from a second terminal.
                </p>
                <div
                  v-for="(connection, i) in vm.connections.filter((c) => c.command)"
                  :key="i"
                  class="connection"
                >
                  <h3>
                    {{
                      connection.kind === 'ssh'
                        ? 'Guest SSH'
                        : connection.kind === 'console'
                        ? 'Serial console'
                        : connection.kind.toUpperCase()
                    }}
                  </h3>
                  <template v-if="connection.tunnel"
                    ><p class="note">1. Open the SSH tunnel</p>
                    <div class="command">
                      <code>{{ connection.tunnel }}</code
                      ><CopyButton :text="connection.tunnel" label="Copy SSH tunnel" />
                    </div>
                    <p class="note">2. Connect to the VM</p></template
                  >
                  <div class="command">
                    <code>{{ connection.command }}</code
                    ><CopyButton :text="connection.command!" label="Copy connection command" />
                  </div>
                  <p v-if="connection.kind === 'console'" class="note">
                    Requires a configured guest serial console and libvirt permission. Press Ctrl+]
                    to leave.
                  </p>
                </div>
                <p class="note">
                  SSH access to this host is required. Install virt-viewer for SPICE/VNC. If a local
                  port is busy, change it in both commands. Use your existing guest key with SSH if
                  required.
                </p>
              </details>
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
