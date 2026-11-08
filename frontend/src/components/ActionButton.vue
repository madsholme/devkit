<script setup lang="ts">
import { Button } from '@madsholme/ui/button'
import { state, act } from '../dashboard'
import { actionLabels } from '../format'
import type { ActionRequest } from '../types'
defineProps<{ request: ActionRequest; label?: string; name?: string }>()
</script>
<template>
  <Button
    v-if="state.session.signed_in"
    variant="outline"
    size="sm"
    :disabled="state.pending"
    @click="act(request, name)"
    >{{ label || actionLabels[request.action] || request.action }}</Button
  >
</template>
