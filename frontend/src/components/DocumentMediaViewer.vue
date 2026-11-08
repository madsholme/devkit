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
type Point = { x: number; y: number }
type Anchor = Point & { imageX: number; imageY: number }
const pointers = new Map<number, Point>()
let dragging: (Point & { left: number; top: number }) | null = null
let pinch: { distance: number; scale: number; anchor: Anchor } | null = null
let zoomRevision = 0
const panning = ref(false)

function fit() {
  const area = viewport.value
  if (!area || !props.media) return
  fitMode.value = true
  zoomRevision++
  scale.value = Math.min(area.clientWidth / props.media.width, area.clientHeight / props.media.height, 8)
  area.scrollTo(0, 0)
}
function anchorAt(x: number, y: number): Anchor {
  const area = viewport.value!
  const media = props.media!
  return {
    x, y,
    imageX: (area.scrollLeft + x - Math.max(0, (area.clientWidth - media.width * scale.value) / 2)) / scale.value,
    imageY: (area.scrollTop + y - Math.max(0, (area.clientHeight - media.height * scale.value) / 2)) / scale.value,
  }
}
function localPoint(point: Point): Point {
  const area = viewport.value!
  const rect = area.getBoundingClientRect()
  return { x: point.x - rect.left - area.clientLeft, y: point.y - rect.top - area.clientTop }
}
async function zoom(value: number, anchor?: Anchor) {
  const area = viewport.value
  const media = props.media
  if (!area || !media) return
  const version = ++zoomRevision
  const point = anchor || anchorAt(area.clientWidth / 2, area.clientHeight / 2)
  fitMode.value = false
  scale.value = Math.max(0.01, Math.min(8, value))
  await nextTick()
  if (media !== props.media || version !== zoomRevision) return
  area.scrollLeft = point.imageX * scale.value + Math.max(0, (area.clientWidth - media.width * scale.value) / 2) - point.x
  area.scrollTop = point.imageY * scale.value + Math.max(0, (area.clientHeight - media.height * scale.value) / 2) - point.y
}
watch(() => props.media, () => {
  failed.value = false
  fitMode.value = true
  zoomRevision++
  pointers.clear()
  dragging = null
  pinch = null
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
  if (!viewport.value || !props.media || !event.deltaY) return
  event.preventDefault()
  event.stopPropagation()
  const point = localPoint({ x: event.clientX, y: event.clientY })
  const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewport.value.clientHeight : 1
  const delta = Math.max(-500, Math.min(500, event.deltaY * unit))
  void zoom(scale.value * Math.exp(-delta * 0.002), anchorAt(point.x, point.y))
}
function rebaseGesture() {
  dragging = null
  pinch = null
  const area = viewport.value
  const points = [...pointers.values()]
  panning.value = points.length > 0
  if (!area || !props.media) return
  if (points.length === 1) {
    dragging = { ...points[0], left: area.scrollLeft, top: area.scrollTop }
  } else if (points.length === 2) {
    const [a, b] = points
    const middle = localPoint({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 })
    pinch = { distance: Math.max(1, Math.hypot(a.x - b.x, a.y - b.y)), scale: scale.value, anchor: anchorAt(middle.x, middle.y) }
  }
}
function startPan(event: PointerEvent) {
  const area = viewport.value
  if (!area || !props.media || event.button !== 0 || pointers.size >= 2) return
  event.preventDefault()
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
  area.setPointerCapture(event.pointerId)
  rebaseGesture()
}
function pan(event: PointerEvent) {
  const area = viewport.value
  if (!area || !pointers.has(event.pointerId)) return
  event.preventDefault()
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
  if (pinch && pointers.size === 2) {
    const [a, b] = [...pointers.values()]
    const middle = localPoint({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 })
    void zoom(pinch.scale * Math.hypot(a.x - b.x, a.y - b.y) / pinch.distance, { ...pinch.anchor, ...middle })
  } else if (dragging) {
    area.scrollLeft = dragging.left + dragging.x - event.clientX
    area.scrollTop = dragging.top + dragging.y - event.clientY
  }
}
function stopPan(event: PointerEvent) {
  if (!pointers.delete(event.pointerId)) return
  if (viewport.value?.hasPointerCapture(event.pointerId)) viewport.value.releasePointerCapture(event.pointerId)
  rebaseGesture()
}
</script>
<template>
  <Dialog :open="Boolean(media)" @update:open="!$event && emit('close')">
    <DialogContent class="document-media-viewer" @keydown="keyboard">
      <DialogHeader class="media-viewer-heading">
        <DialogTitle>{{ media?.title }}</DialogTitle>
        <DialogDescription>Scroll or pinch to zoom. Drag with the mouse or one finger to move around.</DialogDescription>
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
.media-viewer-viewport { min-width: 0; min-height: 0; overflow: auto; overscroll-behavior: contain; touch-action: none; border: 1px solid var(--border); border-radius: var(--radius); background: #fff; color: #222; cursor: grab; }
.media-viewer-viewport.panning { cursor: grabbing; }
.media-viewer-viewport:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.media-viewer-canvas { display: grid; place-items: center; width: max-content; min-width: 100%; min-height: 100%; }
.media-viewer-canvas img { display: block; max-width: none; max-height: none; user-select: none; }
.media-viewer-error { padding: 1rem; }
@media (max-width: 600px) {
  .document-media-viewer[data-slot='dialog-content'] { width: calc(100vw - 1rem); height: calc(100dvh - 1rem); padding: 0.75rem; }
}
</style>
