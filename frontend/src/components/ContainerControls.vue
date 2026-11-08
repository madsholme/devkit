<script setup lang="ts">
import { Button } from '@madsholme/ui/button'
import { computed } from 'vue'
import { state, showDetails, showLogs } from '../dashboard'
import type { Container } from '../types'
import ActionButton from './ActionButton.vue'
const props = defineProps<{ container: Container }>()
const target = computed(() =>
  props.container.registered
    ? { container: props.container.id }
    : { preview: props.container.preview, service: props.container.service }
)
const inspectable = computed(
  () =>
    props.container.registered ||
    (props.container.managed && props.container.preview && props.container.service)
)
</script>
<template>
  <div class="actions">
    <ActionButton
      v-for="action in container.actions || []"
      :key="action"
      :request="{ action: `container-${action}`, container: container.id }"
      :name="container.name"
    /><span v-if="!state.session.signed_in && container.actions?.length" class="muted text-xs"
      >Sign in to manage</span
    ><template v-if="inspectable"
      ><Button variant="ghost" size="sm" @click="showDetails(target, container.name)"
        >Details</Button
      ><Button variant="ghost" size="sm" @click="showLogs(target, container.name)"
        >Logs</Button
      ></template
    >
  </div>
  <p v-if="container.control_note" class="note">{{ container.control_note }}</p>
</template>
