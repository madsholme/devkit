<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@madsholme/ui/dialog'
import { Button } from '@madsholme/ui/button'
import { ZoomIn, ZoomOut, Maximize, Scan } from 'lucide-vue-next'

const props = defineProps<{ media: { src: string; title: string; width: number; height: number } | null }>()
const emit = defineEmits<{ close: [] }>()
const viewport = ref<HTMLElement>()
const scale = ref(1)
const fitMode = ref(true)
const failed = ref(false)
const percent = computed(() => `${Math.round(scale.value * 100)}%`)
const dimensions = computed(() => ({
  width: `${(props.media?.width || 1) * scale.value}px`,
  height: `${(props.media?.height || 1) * scale.value}px`,
}))
let dragging: { id: number; x: number; y: number; left: number; top: number } | null = null
const panning = ref(false)

function fit() {
  const area = viewport.value
  if (!area || !props.media) return
  fitMode.value = true
  scale.value = Math.min(area.clientWidth / props.media.width, area.clientHeight / props.media.height, 8)
  area.scrollTo(0, 0)
}
async function zoom(value: number) {
  const area = viewport.value
  const media = props.media
  if (!area || !media) return
  const old = scale.value
  const x = (area.scrollLeft + area.clientWidth / 2 - Math.max(0, (area.clientWidth - media.width * old) / 2)) / old
  const y = (area.scrollTop + area.clientHeight / 2 - Math.max(0, (area.clientHeight - media.height * old) / 2)) / old
  fitMode.value = false
  scale.value = Math.max(0.01, Math.min(8, value))
  await nextTick()
  if (media !== props.media) return
  area.scrollLeft = x * scale.value + Math.max(0, (area.clientWidth - media.width * scale.value) / 2) - area.clientWidth / 2
  area.scrollTop = y * scale.value + Math.max(0, (area.clientHeight - media.height * scale.value) / 2) - area.clientHeight / 2
}
watch(() => props.media, () => {
  failed.value = false
  fitMode.value = true
  dragging = null
  panning.value = false
})
watch(viewport, (area, _old, cleanup) => {
  if (!area) return
  const observer = new ResizeObserver(() => { if (fitMode.value) fit() })
  observer.observe(area)
  fit()
  cleanup(() => observer.disconnect())
})
function keyboard(event: KeyboardEvent) {
  if (event.ctrlKey || event.metaKey || event.altKey) return
  if (['+', '=', '-', '0', 'f', 'F'].includes(event.key)) {
    event.preventDefault()
    event.stopPropagation()
    if (event.key === '0') void zoom(1)
    else if (event.key.toLowerCase() === 'f') fit()
    else void zoom(scale.value * (event.key === '-' ? 0.8 : 1.25))
  }
}
function wheel(event: WheelEvent) {
  if (!event.ctrlKey) return
  event.preventDefault()
  void zoom(scale.value * (event.deltaY < 0 ? 1.1 : 1 / 1.1))
}
function startPan(event: PointerEvent) {
  const area = viewport.value
  // Touch uses native scrolling; only mouse dragging needs pointer capture.
  if (!area || event.pointerType !== 'mouse' || event.button !== 0) return
  event.preventDefault()
  area.setPointerCapture(event.pointerId)
  dragging = { id: event.pointerId, x: event.clientX, y: event.clientY, left: area.scrollLeft, top: area.scrollTop }
  panning.value = true
}
function pan(event: PointerEvent) {
  if (!dragging || dragging.id !== event.pointerId || !viewport.value) return
  viewport.value.scrollLeft = dragging.left + dragging.x - event.clientX
  viewport.value.scrollTop = dragging.top + dragging.y - event.clientY
}
function stopPan(event: PointerEvent) {
  if (dragging?.id !== event.pointerId) return
  if (viewport.value?.hasPointerCapture(event.pointerId)) viewport.value.releasePointerCapture(event.pointerId)
  dragging = null
  panning.value = false
}
</script>
<template>
  <Dialog :open="Boolean(media)" @update:open="!$event && emit('close')">
    <DialogContent class="document-media-viewer" @keydown="keyboard">
      <DialogHeader class="media-viewer-heading">
        <DialogTitle>{{ media?.title }}</DialogTitle>
        <DialogDescription>Zoom with the controls or Ctrl + scroll. Drag or scroll to move around.</DialogDescription>
      </DialogHeader>
      <div class="media-viewer-toolbar" role="group" aria-label="Media zoom controls">
        <Button variant="outline" size="sm" aria-label="Zoom out" :disabled="scale <= 0.01" @click="zoom(scale * 0.8)"><ZoomOut :size="16" /></Button>
        <span role="status" aria-label="Zoom level" class="media-viewer-percent">{{ percent }}</span>
        <Button variant="outline" size="sm" aria-label="Zoom in" :disabled="scale >= 8" @click="zoom(scale * 1.25)"><ZoomIn :size="16" /></Button>
        <Button variant="outline" size="sm" aria-label="Fit to window" @click="fit"><Maximize :size="15" />Fit</Button>
        <Button variant="outline" size="sm" aria-label="Actual size" @click="zoom(1)"><Scan :size="15" />100%</Button>
      </div>
      <div ref="viewport" class="media-viewer-viewport" :class="{ panning }" role="region" aria-label="Zoomable media" tabindex="0"
        @wheel="wheel" @pointerdown="startPan" @pointermove="pan" @pointerup="stopPan" @pointercancel="stopPan" @lostpointercapture="stopPan">
        <p v-if="failed" class="media-viewer-error" role="alert">This image could not be loaded. Close the viewer and reload the document.</p>
        <div v-else-if="media" class="media-viewer-canvas">
          <img :src="media.src" :alt="media.title" :style="dimensions" referrerpolicy="no-referrer" draggable="false" @error="failed = true" />
        </div>
      </div>
    </DialogContent>
  </Dialog>
</template>
<style>
.document-media-viewer[data-slot='dialog-content'] {
  width: calc(100vw - 2rem);
  max-width: none;
  height: calc(100dvh - 2rem);
  max-height: none;
  grid-template-rows: auto auto minmax(0, 1fr);
  gap: 0.75rem;
  padding: 1rem;
  overflow: hidden;
}
.media-viewer-heading { padding-right: 2rem; min-width: 0; }
.media-viewer-heading [data-slot='dialog-title'] { overflow-wrap: anywhere; }
.media-viewer-toolbar { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
.media-viewer-percent { min-width: 3.5rem; text-align: center; font-variant-numeric: tabular-nums; font-size: 0.875rem; }
.media-viewer-viewport { min-width: 0; min-height: 0; overflow: auto; overscroll-behavior: contain; border: 1px solid var(--border); border-radius: var(--radius); background: #fff; color: #222; cursor: grab; }
.media-viewer-viewport.panning { cursor: grabbing; }
.media-viewer-viewport:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.media-viewer-canvas { display: grid; place-items: center; width: max-content; min-width: 100%; min-height: 100%; }
.media-viewer-canvas img { display: block; max-width: none; max-height: none; user-select: none; }
.media-viewer-error { padding: 1rem; }
@media (max-width: 600px) {
  .document-media-viewer[data-slot='dialog-content'] { width: calc(100vw - 1rem); height: calc(100dvh - 1rem); padding: 0.75rem; }
}
</style>
