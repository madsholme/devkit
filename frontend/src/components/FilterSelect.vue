<script setup lang="ts">
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@madsholme/ui/select'
defineProps<{ label: string; options: { value: string; label: string }[] }>()
const model = defineModel<string>({ default: '' })
</script>
<template>
  <Select
    :model-value="model || 'all'"
    @update:model-value="model = $event === 'all' ? '' : String($event)"
    ><SelectTrigger :aria-label="label" class="filter-select"
      ><SelectValue :placeholder="label" /></SelectTrigger
    ><SelectContent
      ><SelectItem v-for="option in options" :key="option.value" :value="option.value || 'all'">{{
        option.label
      }}</SelectItem></SelectContent
    ></Select
  >
</template>
