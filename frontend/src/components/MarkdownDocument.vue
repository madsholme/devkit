<script setup lang="ts">
import { computed, ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { renderDiagrams } from '../document-diagrams'
import { renderMarkdown } from '../markdown'
import DocumentMediaViewer from './DocumentMediaViewer.vue'
const props = defineProps<{ content: string; path: string; checkout: string }>()
const emit = defineEmits<{ navigate: [path: string, anchor: string] }>()
const rendered = computed(() => renderMarkdown(props.content, props.path, props.checkout))
const body = ref<HTMLElement>()
const media = ref<{ src: string; title: string; width: number; height: number; temporary?: boolean } | null>(null)
function closeMedia() {
  if (media.value?.temporary) URL.revokeObjectURL(media.value.src)
  media.value = null
}
function enhanceMedia() {
  for (const element of body.value?.querySelectorAll<HTMLElement>('img, .document-diagram-rendered') || []) {
    if (element.closest('.document-media-trigger')) continue
    const image = element instanceof HTMLImageElement
    const target = element.closest('a') || element.closest('picture') || element
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'document-media-trigger'
    button.setAttribute('aria-label', image ? `Enlarge image: ${element.alt || 'Picture'}` : 'Enlarge flowchart')
    button.title = 'Click to enlarge and zoom'
    target.replaceWith(button)
    // An image link now opens the viewer, rather than navigating away.
    if (target.tagName === 'A') button.append(...Array.from(target.childNodes))
    else button.append(target)
  }
}
async function openMedia(button: HTMLButtonElement) {
  const image = button.querySelector('img')
  const svg = button.querySelector('svg')
  if (image) {
    if (!image.complete) { try { await image.decode() } catch { return } }
    if (!body.value?.contains(button) || !image.naturalWidth) return
    closeMedia()
    button.focus({ preventScroll: true })
    media.value = { src: image.currentSrc || image.src, title: image.alt || 'Picture', width: image.naturalWidth, height: image.naturalHeight }
  } else if (svg) {
    const box = svg.viewBox.baseVal
    const width = box.width || svg.getBoundingClientRect().width
    const height = box.height || svg.getBoundingClientRect().height
    if (!width || !height) return
    const copy = svg.cloneNode(true) as SVGSVGElement
    copy.setAttribute('width', String(width))
    copy.setAttribute('height', String(height))
    copy.style.maxWidth = 'none'
    copy.style.minWidth = '0'
    // This SVG has already passed the diagram renderer's sanitization. Keep it
    // in an image context and avoid duplicate IDs in the document DOM.
    const src = URL.createObjectURL(new Blob([new XMLSerializer().serializeToString(copy)], { type: 'image/svg+xml' }))
    closeMedia()
    button.focus({ preventScroll: true })
    media.value = { src, title: 'Mermaid flowchart', width, height, temporary: true }
  }
}
let generation = 0
watch(rendered, async () => {
  const version = ++generation
  closeMedia()
  await nextTick()
  if (!body.value) return
  enhanceMedia()
  try {
    await renderDiagrams(body.value, () => version === generation)
    if (version === generation) enhanceMedia()
  }
  catch { /* Source remains readable if the lazy diagram module cannot load. */ }
}, { immediate: true })
onBeforeUnmount(() => { generation++; closeMedia() })
function scrollTo(anchor: string) {
  const target = body.value?.querySelector(`#${CSS.escape('document-' + anchor)}`)
  target?.scrollIntoView({ block: 'start' })
}
function follow(event: MouseEvent) {
  const target = event.target instanceof Element ? event.target : null
  const trigger = target?.closest<HTMLButtonElement>('.document-media-trigger')
  if (trigger && body.value?.contains(trigger)) {
    event.preventDefault()
    void openMedia(trigger)
    return
  }
  const external = target?.closest<HTMLButtonElement>('[data-external-image]')
  if (external && body.value?.contains(external)) {
    const image = document.createElement('img')
    image.alt = external.dataset.imageAlt || 'External image'
    image.referrerPolicy = 'no-referrer'
    image.addEventListener('error', () => {
      const message = document.createElement('span')
      message.textContent = `Image unavailable: ${image.alt}`
      image.replaceWith(message)
    }, { once: true })
    image.addEventListener('load', enhanceMedia, { once: true })
    image.src = external.dataset.externalImage!
    external.replaceWith(image)
    return
  }
  const link = target?.closest('a')
  if (!link || !body.value?.contains(link) || !link.hasAttribute('data-anchor')) return
  event.preventDefault()
  const path = link.dataset.document
  if (path) emit('navigate', path, link.dataset.anchor || '')
  else scrollTo(link.dataset.anchor || '')
}
defineExpose({ scrollTo })
</script>
<template>
  <div class="document-reading">
    <details v-if="rendered.headings.length > 2" class="document-outline">
      <summary>On this page</summary>
      <nav aria-label="Document outline">
        <button
          v-for="heading in rendered.headings"
          :key="heading.id"
          type="button"
          :style="{ paddingLeft: `${Math.min(heading.level - 1, 3)}rem` }"
          @click="scrollTo(heading.id)"
        >
          {{ heading.text }}
        </button>
      </nav>
    </details>
    <article
      ref="body"
      class="markdown-document"
      aria-label="Document content"
      @click="follow"
      v-html="rendered.html"
    ></article>
    <DocumentMediaViewer :media="media" @close="closeMedia" />
    <p v-if="!content.trim()" class="note">This document is empty.</p>
  </div>
</template>
<style>
.document-reading {
  width: 100%;
  min-width: 0;
}
.document-outline {
  border-bottom: 1px solid var(--border);
  padding-bottom: 1rem;
  margin-bottom: 1.5rem;
  font-size: 0.875rem;
}
.document-outline summary {
  cursor: pointer;
  font-weight: 500;
}
.document-outline nav {
  display: grid;
  gap: 0.5rem;
  margin-top: 0.75rem;
}
.document-outline button {
  text-align: left;
  color: var(--primary);
}
.markdown-document {
  line-height: 1.75;
  font-size: 0.9375rem;
  overflow-wrap: anywhere;
}
.markdown-document > :first-child {
  margin-top: 0;
}
.markdown-document h1,
.markdown-document h2,
.markdown-document h3,
.markdown-document h4 {
  font-weight: 600;
  line-height: 1.3;
  margin: 1.8em 0 0.7em;
  scroll-margin-top: 6rem;
}
.markdown-document h1 {
  font-size: 1.9rem;
  letter-spacing: -0.025em;
}
.markdown-document h2 {
  font-size: 1.4rem;
  border-bottom: 1px solid var(--border);
  padding-bottom: 0.4em;
}
.markdown-document h3 {
  font-size: 1.1rem;
}
.markdown-document p,
.markdown-document ul,
.markdown-document ol,
.markdown-document pre,
.markdown-document blockquote,
.markdown-document table {
  margin: 1em 0;
}
.markdown-document ul {
  list-style: disc;
  padding-left: 1.5em;
}
.markdown-document ol {
  list-style: decimal;
  padding-left: 1.5em;
}
.markdown-document li > p {
  margin: 0.35em 0;
}
.markdown-document a {
  color: var(--primary);
  text-decoration: underline;
  text-underline-offset: 0.2em;
}
.markdown-document a[aria-disabled] {
  color: var(--muted-foreground);
  text-decoration: none;
}
.markdown-document code {
  font-family: ui-monospace, monospace;
  font-size: 0.875em;
  background: var(--muted);
  padding: 0.15em 0.3em;
  border-radius: 0.25rem;
}
.markdown-document pre {
  overflow-x: auto;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--muted);
}
.markdown-document pre code {
  padding: 0;
  background: none;
  white-space: pre;
}
.markdown-document blockquote {
  border-left: 3px solid var(--border);
  color: var(--muted-foreground);
  padding-left: 1em;
}
.markdown-document table {
  display: block;
  overflow-x: auto;
  width: 100%;
  border-collapse: collapse;
}
.markdown-document th,
.markdown-document td {
  padding: 0.5em 0.75em;
  border: 1px solid var(--border);
  text-align: left;
}
.markdown-document th {
  font-weight: 600;
}
.markdown-document hr {
  border-top: 1px solid var(--border);
  margin: 2rem 0;
}
.document-image-note {
  color: var(--muted-foreground);
  font-style: italic;
}
.markdown-document img { max-width: 100%; height: auto; border-radius: var(--radius); }
.markdown-document picture { display: block; }
.document-external-image { color: var(--primary); text-decoration: underline; }
.document-diagram { margin: 1.5rem 0; }
.document-diagram-rendered { overflow-x: auto; padding: 1rem; background: #fff; color: #222; border-radius: var(--radius); }
.document-diagram-rendered svg { display: block; min-width: 32rem; max-width: 100%; height: auto; margin: auto; }
.document-diagram details { margin-top: 0.5rem; font-size: 0.8125rem; }
.document-diagram summary { cursor: pointer; color: var(--muted-foreground); }
.document-media-trigger { display: block; max-width: 100%; padding: 0; border: 0; background: transparent; text-align: left; cursor: zoom-in; }
.document-media-trigger:focus-visible { outline: 2px solid var(--ring); outline-offset: 4px; border-radius: var(--radius); }
.document-diagram > .document-media-trigger { width: 100%; }
</style>
