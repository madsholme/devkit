import { computed } from 'vue'
import { state } from './dashboard'
import { projectInventory } from './project-inventory'
export const projectInventories = computed(() =>
  projectInventory(
    state.projects?.projects || [],
    state.containers?.containers || [],
    state.overview,
    state.vms?.vms || []
  )
)
