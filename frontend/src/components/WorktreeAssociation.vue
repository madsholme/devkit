<script setup lang="ts">
import { GitBranch } from 'lucide-vue-next'
import type { Checkout } from '../types'
import { projectLocation } from '../project-inventory'
import { home } from '../format'
defineProps<{ project: string; checkout?: Checkout; note?: string }>()
</script>
<template>
  <template v-if="checkout"
    ><RouterLink
      :to="projectLocation(project, 'worktrees', checkout.path)"
      class="resource-identity project-link"
      ><GitBranch :size="14" />{{
        checkout.branch || (checkout.git ? 'Detached HEAD' : 'Project folder')
      }}</RouterLink
    >
    <p class="path association-path">{{ home(checkout.path) }}</p></template
  >
  <template v-else
    ><span class="muted">Unassigned worktree</span>
    <p class="note">{{ note || 'No unique worktree mapping' }}</p></template
  >
</template>
