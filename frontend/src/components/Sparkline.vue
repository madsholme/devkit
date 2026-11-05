<script setup lang="ts">
import { computed, ref } from 'vue'
const props = defineProps<{ values: { time: number; value: number }[]; label: string }>()
const hovered = ref(-1)
const points = computed(() =>
  props.values
    .map(
      (point, index) =>
        `${props.values.length > 1 ? (index / (props.values.length - 1)) * 100 : 0},${
          40 - Math.min(100, Math.max(0, point.value)) * 0.4
        }`
    )
    .join(' ')
)
function inspect(event: PointerEvent) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  hovered.value = Math.min(
    props.values.length - 1,
    Math.max(0, Math.round(((event.clientX - rect.left) / rect.width) * (props.values.length - 1)))
  )
}
</script>
<template>
  <div
    v-if="values.length > 1"
    class="sparkline"
    @pointermove="inspect"
    @pointerleave="hovered = -1"
  >
    <svg viewBox="0 0 100 40" preserveAspectRatio="none" role="img" :aria-label="label">
      <polyline
        :points="points"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        vector-effect="non-scaling-stroke"
      />
    </svg>
    <p class="note">
      {{
        hovered >= 0 && values[hovered]
          ? `${values[hovered]!.value.toFixed(1)}% · ${new Date(
              values[hovered]!.time * 1000
            ).toLocaleTimeString()}`
          : label
      }}
    </p>
  </div>
  <p v-else class="note">Collecting history…</p>
</template>
