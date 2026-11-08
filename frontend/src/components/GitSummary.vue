<script setup lang="ts">
import { Badge } from '@madsholme/ui/badge'
import { ago, short } from '../format'
import type { GitState } from '../types'
defineProps<{ tree: GitState }>()
</script>
<template>
  <p class="commit-subject">{{ tree.subject || 'No commits' }}</p>
  <div class="metadata">
    <code>{{ short(tree.head) }}</code
    ><span v-if="tree.committed">{{ ago(new Date(tree.committed * 1000).toISOString()) }} ago</span
    ><Badge v-if="tree.changes" variant="warning"
      >{{ tree.changes }} uncommitted {{ tree.changes === 1 ? 'change' : 'changes' }}</Badge
    ><span v-if="!tree.upstream">No upstream</span
    ><span v-else-if="tree.ahead || tree.behind"
      >{{ tree.ahead }} ahead · {{ tree.behind }} behind {{ tree.upstream }}</span
    ><span v-else>In sync with {{ tree.upstream }}</span>
  </div>
</template>
