<script setup lang="ts">
import { computed } from 'vue'
import { History, Search } from 'lucide-vue-next'
import { Input } from '@madsholme/ui/input'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@madsholme/ui/table'
import { filters, state } from '../dashboard'
import { day, home, wordsMatch, historyVerb } from '../format'
import FilterSelect from '../components/FilterSelect.vue'
import StatusBadge from '../components/StatusBadge.vue'
const rows = computed(() =>
  state.history.filter(
    (event) =>
      (filters.outcome !== 'failed' || !event.ok) &&
      wordsMatch(filters.history, [
        event.action,
        historyVerb(event),
        event.preview,
        event.worktree,
        event.detail,
        event.by,
      ])
  )
)
</script>
<template>
  <div class="workspace-surface">
    <div class="workspace-toolbar">
      <div class="search-field">
        <Search :size="16" /><Input
          class="pl-9"
          v-model="filters.history"
          aria-label="Filter history"
          placeholder="Filter by preview, action or address…"
          @keydown.esc="filters.history = ''"
        />
      </div>
      <FilterSelect
        v-model="filters.outcome"
        label="Outcome"
        :options="[
          { value: '', label: 'All outcomes' },
          { value: 'failed', label: 'Failures only' },
        ]"
      />
    </div>
    <Table
      ><TableHeader
        ><TableRow
          ><TableHead>Time</TableHead><TableHead>Action</TableHead><TableHead>Result</TableHead
          ><TableHead>Initiated by</TableHead></TableRow
        ></TableHeader
      ><TableBody
        ><template v-for="(event, index) in rows" :key="`${event.time}-${index}`"
          ><TableRow
            v-if="index === 0 || day(rows[index - 1]!.time) !== day(event.time)"
            class="group-row"
            ><TableCell colspan="4">{{ day(event.time) }}</TableCell></TableRow
          ><TableRow
            ><TableCell
              ><time :datetime="event.time" :title="new Date(event.time).toLocaleString()">{{
                new Date(event.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
              }}</time></TableCell
            ><TableCell
              ><strong>{{ historyVerb(event) }}</strong>
              <code>{{ event.preview || home(event.worktree || '') }}</code>
              <p v-if="event.detail" class="history-detail">{{ event.detail }}</p></TableCell
            ><TableCell><StatusBadge :status="event.ok ? 'done' : 'failed'" /></TableCell
            ><TableCell>{{
              event.by === 'cli' ? 'CLI' : event.by === 'auto' ? 'Automatic' : event.by
            }}</TableCell></TableRow
          ></template
        ></TableBody
      ></Table
    >
    <div v-if="!rows.length" class="empty-state">
      <History :size="28" />
      <h2>{{ filters.history || filters.outcome ? 'No matching events' : 'No activity yet' }}</h2>
      <p>devkit records actions from the CLI, dashboard and automatic maintenance.</p>
    </div>
  </div>
</template>
