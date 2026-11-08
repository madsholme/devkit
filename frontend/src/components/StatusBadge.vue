<script setup lang="ts">
import { computed } from 'vue'
import { Badge } from '@madsholme/ui/badge'
const props = defineProps<{ status: string }>()
const variant = computed(() =>
  ['healthy', 'running', 'up', 'done'].includes(props.status)
    ? 'success'
    : ['unhealthy', 'dead', 'failed'].includes(props.status)
    ? 'destructive'
    : ['starting', 'restarting'].includes(props.status)
    ? 'warning'
    : 'secondary'
)
</script>
<template>
  <Badge :variant="variant" class="status-badge"
    ><span class="status-dot" aria-hidden="true" />{{ status }}</Badge
  >
</template>
