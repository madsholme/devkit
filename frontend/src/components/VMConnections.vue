<script setup lang="ts">
import { computed, ref } from 'vue'
import type { VM } from '../types'
import CopyButton from './CopyButton.vue'
const props = defineProps<{ vm: VM }>()
type Connection = VM['connections'][number]
const selected = ref('')
const networkLabel = (network?: string) => network === 'tailscale' ? 'Away · Tailscale' : network === 'lan' ? 'At home · LAN' : 'Host SSH'
const rank = (kind: string) => ({ spice: 0, vnc: 1, ssh: 2, console: 3 }[kind] ?? 4)
const key = (c: Connection) => `${c.kind}:${c.port || 0}:${c.listen || ''}`
const title = (c: Connection) => c.kind === 'ssh' ? 'Guest SSH' : c.kind === 'console' ? 'Serial console' : c.kind.toUpperCase()
const connections = computed(() => props.vm.connections
  .filter((c) => c.command || c.direct?.length)
  .slice().sort((a, b) => rank(a.kind) - rank(b.kind)))
const active = computed(() => connections.value.find((c) => key(c) === selected.value) || connections.value[0])
</script>
<template>
  <details v-if="active" class="connection-details">
    <summary>Connections <span class="connection-vm-name">for {{ vm.name }}</span></summary>
    <div class="connection-panel">
      <header class="connection-heading">
        <div class="connection-title">
          <h3>{{ title(active) }}</h3>
          <span v-if="active.kind === 'spice'" class="connection-preferred">Preferred display</span>
        </div>
        <label v-if="connections.length > 1" class="connection-picker">
          Connection type
          <select class="filter-select" :aria-label="`Connection type for ${vm.name}`" :value="key(active)" @change="selected = ($event.target as HTMLSelectElement).value">
            <option v-for="c in connections" :key="key(c)" :value="key(c)">{{ title(c) }}{{ c.port ? ` · ${c.port}` : '' }}</option>
          </select>
        </label>
      </header>
      <template v-if="active.kind === 'console'">
        <p class="note">Run on your computer using SSH access to this host.</p>
        <template v-if="active.tunnels?.length">
          <div v-for="route in active.tunnels" :key="route.host" class="connection-console-route">
            <p class="connection-network-label">{{ networkLabel(route.network) }} <span>{{ route.host }}</span></p>
            <div class="command"><code>{{ route.command }}</code><CopyButton :text="route.command" :label="`Copy serial console command · ${networkLabel(route.network)}`" /></div>
          </div>
        </template>
        <div v-else class="command"><code>{{ active.command }}</code><CopyButton :text="active.command!" label="Copy serial console command" /></div>
        <p class="note">Requires a configured guest serial console and libvirt permission. Press Ctrl+] to leave.</p>
      </template>
      <div v-else class="connection-methods">
        <section class="connection-method" aria-label="SSH tunnel">
          <h4>SSH tunnel</h4>
          <p class="note">At home, use the LAN address. Away from home, connect your computer to Tailscale first.</p>
          <template v-if="active.tunnel && active.command">
            <div class="connection-step">
              <p class="connection-step-label">1. Open the tunnel</p>
              <template v-if="active.tunnels?.length">
                <div v-for="route in active.tunnels" :key="route.host" class="connection-tunnel-route">
                  <p class="connection-network-label">{{ networkLabel(route.network) }} <span>{{ route.host }}</span></p>
                  <div class="command"><code>{{ route.command }}</code><CopyButton :text="route.command" :label="`Copy SSH tunnel · ${networkLabel(route.network)}`" /></div>
                </div>
              </template>
              <div v-else class="command"><code>{{ active.tunnel }}</code><CopyButton :text="active.tunnel" label="Copy SSH tunnel" /></div>
              <p class="note">Choose one address, run its command on your computer and leave this terminal open.</p>
            </div>
            <div class="connection-step">
              <p class="connection-step-label">2. Connect to the VM</p>
              <div class="command"><code>{{ active.command }}</code><CopyButton :text="active.command" label="Copy tunneled connection command" /></div>
              <p class="note">Run in a second terminal. If the local port is busy, change it in both commands.</p>
            </div>
          </template>
          <template v-else-if="active.command">
            <div class="command"><code>{{ active.command }}</code><CopyButton :text="active.command" label="Copy connection command" /></div>
          </template>
          <p v-else class="note">Host SSH details are unavailable. Use your host's SSH account and address.</p>
        </section>
        <section class="connection-method" aria-label="Direct connection">
          <h4>Direct <span class="connection-method-context">No SSH tunnel</span></h4>
          <template v-if="active.direct?.length">
            <p class="note">No tunnel needed. Use an address your computer can reach; routing and firewall rules still apply.</p>
            <div v-for="endpoint in active.direct" :key="endpoint.host" class="connection-endpoint">
              <p class="connection-network-label">{{ networkLabel(endpoint.network || 'lan') }}</p>
              <dl class="connection-address">
                <div><dt>Host</dt><dd>{{ endpoint.host }}</dd></div>
                <div><dt>Port</dt><dd>{{ active.port }}</dd></div>
                <div v-if="endpoint.uri" class="connection-uri"><dt>URI</dt><dd>{{ endpoint.uri }}</dd></div>
              </dl>
              <div class="command"><code>{{ endpoint.command }}</code><CopyButton :text="endpoint.command" label="Copy direct connection command" /></div>
            </div>
            <p v-if="!active.direct.some((endpoint) => endpoint.network === 'tailscale')" class="note">No direct Tailscale listener was reported. Away from home, use the Tailscale SSH tunnel.</p>
          </template>
          <p v-else class="note">{{ active.direct_note || 'No direct endpoint was reported. Use the SSH tunnel or check the VM launcher.' }}</p>
        </section>
      </div>
      <p v-if="active.kind !== 'console'" class="connection-footer note">
        {{ active.kind === 'ssh' ? 'Use your guest username and existing SSH key. Add -i to the connection command if needed.' : 'Install virt-viewer to use remote-viewer. Enter the VM display password in the viewer if requested. Direct display connections require a trusted private network.' }}
      </p>
    </div>
  </details>
</template>
