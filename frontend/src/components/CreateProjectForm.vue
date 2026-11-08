<script setup lang="ts">
import { computed, ref } from 'vue'
import { Button } from '@madsholme/ui/button'
import { Input } from '@madsholme/ui/input'
import { Textarea } from '@madsholme/ui/textarea'
import { DialogFooter } from '@madsholme/ui/dialog'
import { state, filters, runAction } from '../dashboard'
import templateGuidance from '../../../project-template/AGENTS.md?raw'

const name = ref('')
const remote = ref('')
const notes = ref('')
const notesBytes = computed(() => new TextEncoder().encode(notes.value).length)
const destination = computed(
  () => `${(state.projects?.directory || '').replace(/\/$/, '')}/${name.value || 'project-name'}`
)
async function create() {
  await runAction({
    action: 'project-create',
    name: name.value,
    remote: remote.value.trim(),
    agent_notes: notes.value,
  })
  if (state.dialog === 'job') filters.projects = ''
}
</script>

<template>
  <form class="dialog-form create-project-form" @submit.prevent="create">
    <div class="create-field">
      <label for="project-name">Project name</label>
      <Input
        id="project-name"
        v-model="name"
        required
        pattern="[a-z0-9][a-z0-9\-]{0,31}"
        maxlength="32"
        placeholder="my-project"
        autocomplete="off"
        :disabled="state.pending"
        aria-describedby="project-destination"
      />
      <p id="project-destination" class="note">
        Create in <code>{{ destination }}</code
        >. Existing folders cannot be used.
      </p>
    </div>
    <div class="create-field">
      <label for="project-remote">Git remote URL <span class="note">(optional)</span></label>
      <Input
        id="project-remote"
        v-model="remote"
        maxlength="2048"
        placeholder="git@github.com:you/my-project.git"
        autocomplete="off"
        :disabled="state.pending"
        aria-describedby="remote-help"
      />
      <p id="remote-help" class="note">
        HTTPS or SSH, without credentials. Sets origin locally; nothing is fetched or pushed.
      </p>
    </div>
    <div class="create-field">
      <label for="project-notes"
        >Additional agent instructions <span class="note">(optional)</span></label
      >
      <Textarea
        id="project-notes"
        v-model="notes"
        :rows="5"
        placeholder="Describe the app, preferred stack and project conventions…"
        :disabled="state.pending"
        aria-describedby="notes-help"
        :aria-invalid="notesBytes > 16384"
      />
      <p id="notes-help" class="note">
        Appended to AGENTS.md after the existing devkit guidance.
        {{ notesBytes.toLocaleString() }} / 16,384 bytes.
      </p>
      <details>
        <summary>View preserved devkit guidance</summary>
        <pre class="template-guidance">{{ templateGuidance }}</pre>
      </details>
    </div>
    <p class="note">
      Includes the sample app, Docker and Compose files, devkit recipe, T3 actions and dotfiles.
      Initializes main with one commit using the host’s Git name and email.
    </p>
    <DialogFooter>
      <Button type="button" variant="outline" :disabled="state.pending" @click="state.dialog = ''"
        >Cancel</Button
      >
      <Button type="submit" :disabled="state.pending || !name || notesBytes > 16384">{{
        state.pending ? 'Creating…' : 'Create Project'
      }}</Button>
    </DialogFooter>
  </form>
</template>

<style scoped>
.create-field {
  display: grid;
  gap: 0.5rem;
  min-width: 0;
}
.create-field label {
  font-weight: 500;
}
.create-field .note {
  margin: 0;
}
.create-field code {
  overflow-wrap: anywhere;
}
summary {
  cursor: pointer;
  font-size: 0.8125rem;
}
.template-guidance {
  max-height: 14rem;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font: 0.75rem/1.5 monospace;
  margin-top: 0.5rem;
}
</style>
