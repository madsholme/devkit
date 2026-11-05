<script setup lang="ts">
import { state } from '../dashboard'
import type { VM } from '../types'
import ActionButton from './ActionButton.vue'
defineProps<{ vm: VM }>()
</script>
<template>
  <div
    v-if="vm.control_id && vm.actions?.length"
    class="actions"
    role="group"
    :aria-label="`${vm.name} controls`"
  >
    <ActionButton
      v-for="action in vm.actions"
      :key="action"
      :request="{ action: `vm-${action}`, vm: vm.control_id }"
      :name="vm.name"
    /><span v-if="!state.session.signed_in" class="note">Sign in to control this VM.</span>
  </div>
</template>
