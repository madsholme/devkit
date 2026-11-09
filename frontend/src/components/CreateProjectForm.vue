<script setup lang="ts">
import { computed, ref } from 'vue'
import { Button } from '@madsholme/ui/button'
import { Checkbox } from '@madsholme/ui/checkbox'
import { Input } from '@madsholme/ui/input'
import { Textarea } from '@madsholme/ui/textarea'
import { DialogFooter } from '@madsholme/ui/dialog'
import { state, filters, runAction } from '../dashboard'
import templateGuidance from '../../../project-template/AGENTS.md?raw'

const name = ref('')
const remote = ref('')
const notes = ref('')
const github = ref(false)
const githubOwner = ref('')
const githubRepo = ref('')
const githubPublic = ref(false)
const githubPush = ref(false)
const notesBytes = computed(() => new TextEncoder().encode(notes.value).length)
const destination = computed(
  () => `${(state.projects?.directory || '').replace(/\/$/, '')}/${name.value || 'project-name'}`
)
async function create() {
  await runAction({
    action: 'project-create',
    name: name.value,
    remote: github.value ? '' : remote.value.trim(),
    ...(github.value ? {
      github_owner: githubOwner.value.trim(),
      github_repo: githubRepo.value.trim() || name.value,
      github_public: githubPublic.value,
      github_push: githubPush.value,
    } : {}),
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
    <label class="checkbox-label">
      <Checkbox :model-value="github" :disabled="state.pending" @update:model-value="github = $event === true" />
      Create GitHub repository
    </label>
    <template v-if="github">
      <div class="create-field">
        <label for="github-owner">GitHub owner</label>
        <Input id="github-owner" v-model="githubOwner" required maxlength="39"
          pattern="[a-zA-Z0-9]([a-zA-Z0-9\-]{0,37}[a-zA-Z0-9])?"
          placeholder="Your username or organization" autocomplete="off" :disabled="state.pending" />
      </div>
      <div class="create-field">
        <label for="github-repo">Repository name</label>
        <Input id="github-repo" v-model="githubRepo" maxlength="100" pattern="[a-zA-Z0-9_.\-]{1,100}"
          :placeholder="name || 'my-project'" autocomplete="off" :disabled="state.pending" aria-describedby="github-repo-help" />
        <p id="github-repo-help" class="note">Defaults to the project name. Must be a new repository, without the .git suffix.</p>
      </div>
      <label class="checkbox-label">
        <Checkbox :model-value="githubPublic" :disabled="state.pending" @update:model-value="githubPublic = $event === true" />
        Public repository
      </label>
      <p class="note">{{ githubPublic ? 'Anyone can see this repository and anything pushed to it.' : 'Private: access is limited to people you authorize on GitHub.' }}</p>
      <label class="checkbox-label">
        <Checkbox :model-value="githubPush" :disabled="state.pending" @update:model-value="githubPush = $event === true" />
        Push initial commit
      </label>
      <p class="note">{{ githubPush ? 'Uploads the template and additional agent instructions to GitHub and tracks origin/main.' : 'Creates an empty GitHub repository and sets origin. Your initial commit stays local.' }}</p>
      <p class="note">Uses the host’s configured GitHub integration token. Configure it under Secrets → Integrations first; it must allow repository creation for this owner{{ githubPush ? ' and writing contents' : '' }}.</p>
    </template>
    <div v-else class="create-field">
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
