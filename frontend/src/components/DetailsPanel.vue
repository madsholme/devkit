<script setup lang="ts">
import { computed, ref } from 'vue'
import { Input } from '@madsholme/ui/input'
import { Table, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { state } from '../dashboard'
const filter = ref('')
const command = (parts: string[]) =>
  (parts || []).map((part) => (/\s/.test(part) ? JSON.stringify(part) : part)).join(' ')
const sections = computed(() => {
  const d = state.details
  if (!d) return []
  return [
    {
      title: 'Container',
      rows: [
        ['Name', d.name],
        ['ID', d.id],
        ['Status', `${d.status}${d.status === 'exited' ? ` (exit ${d.exit_code})` : ''}`],
        ['Image', d.image],
        ['Image ID', d.image_id],
        ['Created', d.created],
        ['Started', d.started],
        ['Restart policy', d.restart_policy],
        ['Restarts', d.restart_count],
        ['User', d.user],
        ['Working directory', d.working_dir],
        ['Entrypoint', command(d.entrypoint)],
        ['Command', command(d.command)],
      ],
    },
    ...(d.health?.status
      ? [
          {
            title: 'Health',
            rows: [
              ['Status', d.health.status],
              ['Check', command(d.health.test.filter((v, i) => i || !v.startsWith('CMD')))],
              ['Failing streak', d.health.failing_streak],
              ['Last exit', d.health.last_exit],
              ['Last output', d.health.last_output],
            ],
          },
        ]
      : []),
    {
      title: 'Ports',
      rows: d.ports.map((p) => [
        `${p.port}/${p.protocol}`,
        p.routed ? 'Preview URL (Traefik)' : 'Internal',
      ]),
    },
    { title: 'Networks', rows: d.networks.map((n) => [n.name, `${n.ip} ${n.aliases.join(', ')}`]) },
    {
      title: 'Mounts',
      rows: d.mounts.map((m) => [
        m.destination,
        `${m.type} ${m.source}${m.rw ? '' : ' (read-only)'}`,
      ]),
    },
    { title: `Environment (${d.env.length})`, rows: d.env.map((e) => [e.name, e.value]) },
    { title: `Labels (${Object.keys(d.labels).length})`, rows: Object.entries(d.labels) },
  ].map((section) => ({
    ...section,
    rows: section.rows.filter((row) =>
      row.join(' ').toLowerCase().includes(filter.value.toLowerCase())
    ),
  }))
})
</script>
<template>
  <div v-if="state.details" class="details-panel">
    <Input
      v-model="filter"
      aria-label="Filter container details"
      placeholder="Filter details, environment or labels…"
    />
    <section v-for="section in sections" :key="section.title">
      <h3>{{ section.title }}</h3>
      <Table
        ><TableBody
          ><TableRow v-for="(row, index) in section.rows" :key="index"
            ><TableCell class="detail-key">{{ row[0] }}</TableCell
            ><TableCell class="detail-value">{{
              row[1] === '' || row[1] == null ? '—' : row[1]
            }}</TableCell></TableRow
          ></TableBody
        ></Table
      >
      <p v-if="!section.rows.length" class="note">
        None{{ filter ? ' matching this filter' : '' }}.
      </p>
    </section>
  </div>
  <p v-else-if="!state.dialogError" class="note">Loading container details…</p>
</template>
