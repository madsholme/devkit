<script setup lang="ts">
import { computed, ref } from 'vue'
import { renderMarkdown } from '../markdown'
const props = defineProps<{ content: string; path: string }>()
const emit = defineEmits<{ navigate: [path: string, anchor: string] }>()
const rendered = computed(() => renderMarkdown(props.content, props.path))
const body = ref<HTMLElement>()
function scrollTo(anchor: string) {
  const target = body.value?.querySelector(`#${CSS.escape('document-' + anchor)}`)
  target?.scrollIntoView({ block: 'start' })
}
function follow(event: MouseEvent) {
  const link = (event.target as HTMLElement).closest('a')
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
</style>
