<script setup lang="ts">
import { GitBranch, Container as ContainerIcon, ExternalLink } from 'lucide-vue-next'
import { Button } from '@madsholme/ui/button'
import { Badge } from '@madsholme/ui/badge'
import { state } from '../dashboard'
import { home, safeURL, up } from '../format'
import type { Checkout } from '../types'
import ActionButton from './ActionButton.vue'
import GitSummary from './GitSummary.vue'
import StatusBadge from './StatusBadge.vue'
import ContainerControls from './ContainerControls.vue'
defineProps<{ tree: Checkout; label: string }>()
</script>
<template>
  <article class="checkout-row">
    <div class="row-heading">
      <div>
        <h3>
          <GitBranch v-if="tree.git" :size="16" />{{ label }}
          <code v-if="label !== tree.branch" class="muted">{{
            tree.branch || (tree.git ? 'detached' : '')
          }}</code>
        </h3>
        <p class="path">{{ home(tree.path) }}</p>
      </div>
      <div class="actions">
        <ActionButton
          v-if="tree.docker && !tree.previews.some((p) => p.mode === 'live')"
          :request="{ action: 'up', worktree: tree.path }"
        />
      </div>
    </div>
    <GitSummary v-if="tree.git" :tree="tree" />
    <div
      v-if="tree.git"
      class="actions git-actions"
      role="group"
      :aria-label="`Git controls for ${tree.branch || 'checkout'}`"
    >
      <ActionButton
        v-for="action in tree.git_controls?.actions || []"
        :key="action"
        :request="{ action: `git-${action}`, worktree: tree.path }"
      />
    </div>
    <p v-if="tree.git_controls?.note" class="note">{{ tree.git_controls.note }}</p>
    <p v-else-if="tree.git && !state.session.signed_in" class="note">
      Sign in to sync this checkout with origin.
    </p>
    <p v-if="tree.note" class="note">{{ tree.note }}</p>
    <p
      v-if="
        tree.docker && !state.session.signed_in && !tree.previews.some((p) => p.mode === 'live')
      "
      class="note"
    >
      Sign in to launch containers.
    </p>
    <div v-for="preview in tree.previews" :key="preview.name" class="resource-line">
      <div class="resource-identity">
        <ContainerIcon :size="16" /><Badge variant="secondary">{{ preview.mode }}</Badge
        ><strong>{{ preview.name }}</strong
        ><StatusBadge :status="preview.status" />
      </div>
      <div class="actions">
        <Button v-if="up(preview.status) && safeURL(preview.url)" as-child variant="ghost" size="sm"
          ><a :href="safeURL(preview.url)" target="_blank" rel="noopener"
            >Open app<ExternalLink :size="14" /></a></Button
        ><ActionButton
          :request="{ action: up(preview.status) ? 'stop' : 'start', preview: preview.name }"
          :label="up(preview.status) ? 'Stop containers' : 'Start containers'"
        />
      </div>
    </div>
    <p v-if="tree.containers?.length" class="section-label">
      Existing containers · original storage and endpoints retained
    </p>
    <div v-for="container in tree.containers || []" :key="container.id" class="resource-line">
      <div>
        <div class="resource-identity">
          <ContainerIcon :size="16" /><strong>{{ container.service || container.name }}</strong
          ><StatusBadge :status="container.status" />
        </div>
        <p class="path">{{ container.name }}</p>
        <p class="path">{{ container.ports.join(' · ') }}</p>
      </div>
      <ContainerControls :container="container" />
    </div>
  </article>
</template>
