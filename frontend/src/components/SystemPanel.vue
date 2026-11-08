<script setup lang="ts">
import { computed } from 'vue'
import { Progress } from '@madsholme/ui/progress'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { state, currentSample } from '../dashboard'
import { bytes, percent } from '../format'
import Sparkline from './Sparkline.vue'
import InventoryNotice from './InventoryNotice.vue'
const metrics = computed(() => {
  const s = currentSample.value
  return s
    ? [
        {
          name: 'CPU',
          value: `${s.cpu.toFixed(0)}%`,
          percent: s.cpu,
          detail: `Load ${s.load.map((v) => v.toFixed(2)).join(' · ')}`,
          history: state.resources!.history.map((h) => ({ time: h.t, value: h.cpu })),
        },
        {
          name: 'Memory',
          value: bytes(s.mem_used),
          percent: percent(s.mem_used, s.mem_total),
          detail: `of ${bytes(s.mem_total)}`,
          history: state.resources!.history.map((h) => ({
            time: h.t,
            value: percent(h.mem_used, h.mem_total),
          })),
        },
        {
          name: 'Disk /',
          value: bytes(s.disk_used),
          percent: percent(s.disk_used, s.disk_total),
          detail: `${bytes(s.disk_total - s.disk_used)} free`,
          history: [],
        },
        {
          name: 'Swap',
          value: bytes(s.swap_used),
          percent: percent(s.swap_used, s.swap_total),
          detail: `of ${bytes(s.swap_total)}`,
          history: [],
        },
      ]
    : []
})
</script>
<template>
  <InventoryNotice :text="state.errors.resources" error />
  <p v-if="state.resources" class="note">
    {{ state.resources.cores }} cores · up {{ Math.floor(state.resources.uptime / 86400) }}d
    {{ Math.floor((state.resources.uptime % 86400) / 3600) }}h · sampled every
    {{ state.resources.interval }}s
  </p>
  <div class="system-metrics">
    <section v-for="metric in metrics" :key="metric.name">
      <h3>{{ metric.name }}</h3>
      <p class="metric-value">{{ metric.value }}</p>
      <Progress
        :model-value="Math.min(100, metric.percent)"
        :aria-label="`${metric.name} usage`"
        :color="metric.percent >= 90 ? 'destructive' : metric.percent >= 75 ? 'warning' : 'primary'"
      />
      <p class="note">
        {{ metric.detail
        }}<span v-if="metric.percent >= 75">
          · {{ metric.percent >= 90 ? 'Critical' : 'High' }} usage</span
        >
      </p>
      <Sparkline
        v-if="metric.history.length"
        :values="metric.history"
        :label="`${metric.name} · last 10 minutes`"
      />
    </section>
  </div>
  <h3 class="subsection-heading">Container usage</h3>
  <Table
    ><TableHeader
      ><TableRow
        ><TableHead>Container</TableHead><TableHead>CPU</TableHead><TableHead>Memory</TableHead
        ><TableHead>Network</TableHead><TableHead>Block I/O</TableHead
        ><TableHead>PIDs</TableHead></TableRow
      ></TableHeader
    ><TableBody
      ><TableRow v-for="container in state.resources?.containers || []" :key="container.name"
        ><TableCell>{{
          container.preview ? `${container.preview} · ${container.service}` : container.name
        }}</TableCell
        ><TableCell>{{ container.cpu.toFixed(1) }}%</TableCell
        ><TableCell>{{ container.mem }}</TableCell
        ><TableCell>{{ container.net }}</TableCell
        ><TableCell>{{ container.block }}</TableCell
        ><TableCell>{{ container.pids }}</TableCell></TableRow
      ></TableBody
    ></Table
  >
  <h3 class="subsection-heading">Host processes</h3>
  <Table
    ><TableHeader
      ><TableRow
        ><TableHead>PID / user</TableHead><TableHead>Command</TableHead><TableHead>CPU</TableHead
        ><TableHead>Memory</TableHead></TableRow
      ></TableHeader
    ><TableBody
      ><TableRow v-for="process in state.resources?.processes || []" :key="process.pid"
        ><TableCell
          ><code>{{ process.pid }}</code>
          <p class="note">{{ process.user }}</p></TableCell
        ><TableCell
          ><strong>{{ process.name }}</strong>
          <p class="path table-path" :title="process.command">{{ process.command }}</p></TableCell
        ><TableCell>{{ process.cpu.toFixed(1) }}%</TableCell
        ><TableCell>{{ bytes(process.rss) }}</TableCell></TableRow
      ></TableBody
    ></Table
  >
  <h3 class="subsection-heading">Docker disk</h3>
  <Table
    ><TableHeader
      ><TableRow
        ><TableHead>Type</TableHead><TableHead>Size</TableHead
        ><TableHead>Reclaimable</TableHead></TableRow
      ></TableHeader
    ><TableBody
      ><TableRow v-for="disk in state.overview?.host.disk || []" :key="disk.type"
        ><TableCell>{{ disk.type }}</TableCell
        ><TableCell>{{ disk.size }}</TableCell
        ><TableCell>{{ disk.reclaimable }}</TableCell></TableRow
      ></TableBody
    ></Table
  >
</template>
