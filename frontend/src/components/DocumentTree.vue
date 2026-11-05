<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { TreeRoot, TreeItem, type TreeItemSelectEvent } from 'reka-ui'
import {
  ChevronRight,
  Folder,
  FolderOpen,
  FileText,
  ChevronsUpDown,
  ChevronsDownUp,
} from 'lucide-vue-next'
import { Button } from '@madsholme/ui/button'
interface Node {
  path: string
  name: string
  editable?: boolean
  children?: Node[]
}
const props = defineProps<{
  files: { path: string; editable: boolean }[]
  selected?: string
  scope: string
  searching: boolean
  disabled: boolean
}>()
const emit = defineEmits<{ navigate: [path: string] }>()
const expanded = ref<string[]>(['docs'])
let lastSelected = ''
const tree = computed(() => {
  const roots: Node[] = []
  for (const file of props.files) {
    let siblings = roots
    const parts = file.path.split('/')
    for (let i = 0; i < parts.length; i++) {
      const path = parts.slice(0, i + 1).join('/')
      let node = siblings.find((n) => n.path === path)
      if (!node) {
        node = {
          path,
          name: parts[i],
          ...(i < parts.length - 1 ? { children: [] } : { editable: file.editable }),
        }
        siblings.push(node)
      }
      if (node.children) siblings = node.children
    }
  }
  function sort(nodes: Node[]) {
    nodes.sort(
      (a, b) =>
        Number(Boolean(b.children)) - Number(Boolean(a.children)) ||
        a.name.localeCompare(b.name, undefined, { numeric: true })
    )
    nodes.forEach((n) => n.children && sort(n.children))
  }
  sort(roots)
  return roots
})
const folders = computed(() => {
  const paths: string[] = []
  function visit(nodes: Node[]) {
    for (const n of nodes)
      if (n.children) {
        paths.push(n.path)
        visit(n.children)
      }
  }
  visit(tree.value)
  return paths
})
const visibleExpanded = computed(() => (props.searching ? folders.value : expanded.value))
watch(
  () => props.scope,
  () => {
    expanded.value = ['docs']
    lastSelected = ''
  }
)
watch(
  () => [props.scope, props.selected],
  () => {
    if (!props.selected || props.selected === lastSelected) return
    lastSelected = props.selected
    const parts = props.selected.split('/')
    expanded.value = [
      ...new Set([
        ...expanded.value,
        ...parts.slice(0, -1).map((_, i) => parts.slice(0, i + 1).join('/')),
      ]),
    ]
  },
  { immediate: true }
)
function select(event: TreeItemSelectEvent<Node>, node: Node) {
  // Selection follows the actual opened document, including cancelled dirty-draft navigation.
  event.preventDefault()
  if (props.disabled) return
  if (!node.children) emit('navigate', node.path)
  else if (event.detail.originalEvent instanceof KeyboardEvent && !props.searching) {
    expanded.value = expanded.value.includes(node.path)
      ? expanded.value.filter((p) => p !== node.path)
      : [...expanded.value, node.path]
  }
}
function updateExpanded(paths: string[]) {
  if (!props.searching && !props.disabled) expanded.value = paths
}
</script>
<template>
  <div class="document-tree-controls">
    <Button
      variant="ghost"
      size="sm"
      :disabled="disabled || searching || !folders.length"
      @click="expanded = [...folders]"
      ><ChevronsUpDown :size="14" />Expand all</Button
    >
    <Button
      variant="ghost"
      size="sm"
      :disabled="disabled || searching || !folders.length || !expanded.length"
      @click="expanded = []"
      ><ChevronsDownUp :size="14" />Collapse all</Button
    >
  </div>
  <p v-if="searching" class="tree-search-note note">Matching folders expanded</p>
  <TreeRoot
    v-slot="{ flattenItems }"
    class="document-tree"
    aria-label="Markdown files"
    :aria-disabled="disabled"
    :items="tree"
    :get-key="(node: Node) => node.path"
    :model-value="{ path: selected || '' }"
    :expanded="visibleExpanded"
    :disabled="disabled"
    @update:expanded="updateExpanded"
  >
    <TreeItem
      v-for="item in flattenItems"
      :key="item._id"
      v-slot="{ isExpanded }"
      v-bind="item.bind"
      class="document-tree-row"
      :style="{ paddingLeft: `${0.5 + (item.level - 1) * 1}rem` }"
      :title="item.value.path"
      :aria-label="item.value.name"
      :aria-current="!item.hasChildren && selected === item.value.path ? 'page' : undefined"
      @select="select($event, item.value)"
      @toggle="(searching || disabled) && $event.preventDefault()"
    >
      <ChevronRight
        v-if="item.hasChildren"
        :size="13"
        class="tree-chevron"
        :class="{ 'tree-chevron-open': isExpanded }"
      /><span v-else class="tree-file-spacer" />
      <component
        :is="item.hasChildren ? (isExpanded ? FolderOpen : Folder) : FileText"
        :size="15"
        class="tree-icon"
      />
      <span class="tree-name">{{ item.value.name }}</span>
      <span v-if="!item.hasChildren && !item.value.editable" class="sr-only"
        >Read-only agent guidance</span
      >
    </TreeItem>
  </TreeRoot>
</template>
<style scoped>
.document-tree-controls {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.25rem;
  padding: 0 0.5rem 0.5rem;
}
.document-tree-controls :deep(button) {
  font-size: 0.75rem;
  padding-inline: 0.4rem;
  gap: 0.3rem;
}
.tree-search-note {
  padding: 0 1rem 0.5rem;
  font-size: 0.75rem;
}
.document-tree {
  padding: 0 0.5rem 0.5rem;
  max-height: 65vh;
  overflow: auto;
}
.document-tree-row {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.55rem 0.5rem;
  border-radius: var(--radius);
  font-size: 0.8125rem;
  cursor: pointer;
  min-width: max-content;
}
.document-tree-row:hover {
  background: var(--muted);
}
.document-tree-row[aria-current] {
  background: var(--accent);
  color: var(--accent-foreground);
  font-weight: 600;
}
.document-tree-row:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: -2px;
}
.tree-chevron,
.tree-icon,
.tree-file-spacer {
  flex-shrink: 0;
}
.tree-chevron-open {
  transform: rotate(90deg);
}
.tree-file-spacer {
  width: 13px;
}
.tree-name {
  max-width: 18rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (max-width: 760px) {
  .document-tree {
    max-height: 16rem;
  }
}
</style>
