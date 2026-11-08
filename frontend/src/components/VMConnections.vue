<script setup lang="ts">
import type { VM } from '../types'
import CopyButton from './CopyButton.vue'
defineProps<{ vm: VM }>()
</script>
<template>
  <details v-if="vm.connections.some((c) => c.command)" class="connection-details">
    <summary>Connect privately</summary>
    <p class="note">
      Run these commands on your computer. Keep the tunnel terminal open, then connect from a second
      terminal.
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
        Requires a configured guest serial console and libvirt permission. Press Ctrl+] to leave.
      </p>
    </div>
    <p class="note">
      SSH access to this host is required. Install virt-viewer for SPICE/VNC. If a local port is
      busy, change it in both commands. Use your existing guest key with SSH if required.
    </p>
  </details>
</template>
