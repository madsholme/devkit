<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import { Copy, Check } from 'lucide-vue-next'
import { Button } from '@madsholme/ui/button'
import { copyText, errorMessage } from '../format'
import { state } from '../dashboard'
const props = withDefaults(defineProps<{ text: string; label?: string }>(), { label: 'Copy' })
const copied = ref(false)
let timer: ReturnType<typeof setTimeout>
async function copy() {
  try {
    await copyText(props.text)
    copied.value = true
    clearTimeout(timer)
    timer = setTimeout(() => (copied.value = false), 1400)
  } catch (error) {
    state.error = errorMessage(error)
  }
}
onBeforeUnmount(() => clearTimeout(timer))
</script>
<template>
  <Button
    variant="ghost"
    size="sm"
    :aria-label="copied ? 'Copied' : label"
    :title="label"
    @click="copy"
    ><Check v-if="copied" :size="14" /><Copy v-else :size="14" /><span class="sr-only">{{
      copied ? 'Copied' : label
    }}</span></Button
  >
</template>
