<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@madsholme/ui/dialog'
import { Button } from '@madsholme/ui/button'
import { Input } from '@madsholme/ui/input'
import { Checkbox } from '@madsholme/ui/checkbox'
import { state, confirmationInfo, runAction, signIn, loadLogs, visibleLogs } from '../dashboard'
import CreateProjectForm from './CreateProjectForm.vue'
import DetailsPanel from './DetailsPanel.vue'
import SystemPanel from './SystemPanel.vue'
import CopyButton from './CopyButton.vue'
import FilterSelect from './FilterSelect.vue'
import InventoryNotice from './InventoryNotice.vue'
import StatusBadge from './StatusBadge.vue'
function setFollow(value: boolean | 'indeterminate') {
  state.follow = value === true
  void loadLogs()
}
const password = ref('')
const hours = ref(1)
const host = ref('')
const output = ref<HTMLElement>()
const titles: Record<string, string> = {
  'create-project': 'Create Project',
  signin: 'Sign in',
  action: 'Confirm action',
  job: 'Action output',
  details: 'Container details',
  logs: 'Logs',
  pair: 'Pair device',
  system: 'System monitor',
}
const descriptions: Record<string, string> = {
  'create-project': 'Start a new project from the devkit template.',
  signin: 'Sign in to manage this host. Your session lasts seven days.',
  action: 'Review this operation before continuing.',
  job: 'Live output from the devkit CLI. Closing this dialog does not cancel the job.',
  details: 'Container configuration, health and masked environment values.',
  logs: 'Recent container output. Follow to stream new lines.',
  pair: 'Connect one phone or computer to T3 Code.',
  system:
    'Host resources and container usage. Container and process sampling runs while this view is open.',
}
const title = computed(() =>
  state.dialog === 'action'
    ? `${confirmationInfo.value?.title} ${state.actionName}`
    : state.dialog === 'job'
    ? state.job.title
    : titles[state.dialog] || ''
)
watch(
  () => state.dialog,
  () => {
    password.value = ''
    hours.value = state.session.share?.default_hours || 1
    host.value = ''
  }
)
watch(
  () => [visibleLogs.value, state.job.output],
  async () => {
    const element = output.value
    const atBottom =
      !element || element.scrollHeight - element.scrollTop - element.clientHeight < 48
    await nextTick()
    if (atBottom && output.value) output.value.scrollTop = output.value.scrollHeight
  }
)
const shareOptions = computed(() => [
  {
    value: '',
    label: `${state.session.share?.prefix || ''}${state.action?.preview || ''}.${
      state.session.share?.domain || ''
    }`,
  },
  ...(state.session.share?.hosts || []).map((value) => ({
    value,
    label: `${value}.${state.session.share!.domain}`,
  })),
])
async function confirmAction() {
  if (!state.action || !confirmationInfo.value) return
  const request = { ...state.action, confirm: confirmationInfo.value.confirm }
  if (request.action === 'share') {
    request.host = host.value
    request.hours = Number(hours.value)
  }
  await runAction(request)
}
const qrPath = computed(
  () =>
    state.pairing?.qr
      .flatMap((row, y) =>
        [...row].flatMap((cell, x) => (cell === '1' ? [`M${x + 4} ${y + 4}h1v1h-1z`] : []))
      )
      .join('') || ''
)
</script>
<template>
  <Dialog :open="Boolean(state.dialog)" @update:open="!$event && (state.dialog = '')"
    ><DialogContent
      :class="[
        'dashboard-dialog',
        { 'wide-dialog': ['system', 'logs', 'details', 'job'].includes(state.dialog) },
      ]"
      :show-close="!state.pending"
      @escape-key-down="state.pending && $event.preventDefault()"
      @interact-outside="state.pending && $event.preventDefault()"
      ><DialogHeader
        ><DialogTitle>{{ title }}</DialogTitle
        ><DialogDescription>{{ descriptions[state.dialog] }}</DialogDescription></DialogHeader
      ><InventoryNotice :text="state.dialogError" error />
      <form v-if="state.dialog === 'signin'" class="dialog-form" @submit.prevent="signIn(password)">
        <label for="signin-password">Dashboard password</label
        ><Input
          id="signin-password"
          v-model="password"
          type="password"
          autocomplete="current-password"
          required
          autofocus
        /><DialogFooter
          ><Button
            type="button"
            variant="outline"
            :disabled="state.pending"
            @click="state.dialog = ''"
            >Cancel</Button
          ><Button type="submit" :disabled="state.pending">{{
            state.pending ? 'Signing in…' : 'Sign in'
          }}</Button></DialogFooter
        >
      </form>
      <CreateProjectForm v-else-if="state.dialog === 'create-project'" />
      <form
        v-else-if="state.dialog === 'action'"
        class="dialog-form"
        @submit.prevent="confirmAction"
      >
        <p>{{ confirmationInfo?.description }}</p>
        <template v-if="state.action?.action === 'share' && state.session.share"
          ><label>Address</label
          ><FilterSelect v-model="host" label="Public address" :options="shareOptions" /><label
            for="share-hours"
            >Hours (1–{{ state.session.share.max_hours }})</label
          ><Input
            id="share-hours"
            v-model="hours"
            type="number"
            min="1"
            :max="state.session.share.max_hours"
            required /></template
        ><DialogFooter
          ><Button
            type="button"
            variant="outline"
            :disabled="state.pending"
            @click="state.dialog = ''"
            >Cancel</Button
          ><Button
            type="submit"
            :variant="confirmationInfo?.danger ? 'destructive' : 'default'"
            :disabled="state.pending"
            >{{ state.pending ? 'Starting…' : confirmationInfo?.title }}</Button
          ></DialogFooter
        >
      </form>
      <template v-else-if="state.dialog === 'job'"
        ><StatusBadge :status="state.job.status" />
        <pre ref="output" class="log-output" aria-live="off">{{
          state.job.output || 'Waiting for output…'
        }}</pre>
        <p class="sr-only" role="status">{{ state.job.status }}</p></template
      >
      <template v-else-if="state.dialog === 'logs'"
        ><p class="path">{{ state.logTitle }}</p>
        <div class="actions">
          <label class="checkbox-label"
            ><Checkbox :model-value="state.follow" @update:model-value="setFollow" />Follow</label
          ><label class="checkbox-label"
            ><Checkbox
              :model-value="state.hideHealth"
              @update:model-value="state.hideHealth = Boolean($event)"
            />Hide health checks</label
          ><Button size="sm" variant="outline" @click="loadLogs">Reload</Button
          ><span v-if="state.streaming" class="note" role="status">Following live</span>
        </div>
        <pre ref="output" class="log-output">{{
          visibleLogs || (state.streaming ? 'Waiting for output…' : 'No output')
        }}</pre>
      </template>
      <template v-else-if="state.dialog === 'details'"
        ><p class="path">{{ state.logTitle }}</p>
        <DetailsPanel
      /></template>
      <SystemPanel v-else-if="state.dialog === 'system'" />
      <template v-else-if="state.dialog === 'pair'"
        ><template v-if="state.pairing"
          ><svg
            class="pair-qr"
            :viewBox="`0 0 ${state.pairing.qr.length + 8} ${state.pairing.qr.length + 8}`"
            role="img"
            aria-label="QR code with the pairing link"
            shape-rendering="crispEdges"
          >
            <rect width="100%" height="100%" fill="white" />
            <path :d="qrPath" fill="black" />
          </svg>
          <p class="note">
            Scan with your other device or open the link there. It works once, until
            {{
              new Date(state.pairing.expires).toLocaleTimeString([], {
                hour: '2-digit',
                minute: '2-digit',
              })
            }}.
          </p>
          <div class="command">
            <Input :model-value="state.pairing.url" readonly aria-label="Pairing link" /><CopyButton
              :text="state.pairing.url"
              label="Copy pairing link"
            />
          </div>
          <p v-if="state.pairing.code">
            Or enter <strong class="font-mono">{{ state.pairing.code }}</strong> in T3 Code.
          </p></template
        >
        <p v-else-if="!state.dialogError" class="note">
          Asking T3 Code for a pairing link…
        </p></template
      >
      <DialogFooter v-if="!['signin', 'action', 'create-project'].includes(state.dialog)"
        ><Button variant="outline" @click="state.dialog = ''">Close</Button></DialogFooter
      ></DialogContent
    ></Dialog
  >
</template>
