<script setup lang="ts">
import { AlertTriangle } from 'lucide-vue-next'
defineProps<{ text?: string; error?: boolean }>()
</script>
<template>
  <div
    v-if="text"
    :role="error ? 'alert' : 'status'"
    class="notice"
    :class="{ 'notice-error': error }"
  >
    <AlertTriangle :size="16" aria-hidden="true" /><span>{{ text }}</span>
  </div>
</template>
