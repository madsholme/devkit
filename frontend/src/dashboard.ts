import { computed, reactive, watch } from 'vue'
import { APIError, confirmation, post, request, targetQuery } from './api'
import { actionLabels, errorMessage, store } from './format'
import type {
  ActionRequest,
  Container,
  ContainerDetails,
  HistoryEvent,
  LogTarget,
  Overview,
  Pairing,
  Project,
  Resources,
  Session,
  VM,
} from './types'
export const tabs = ['projects', 'previews', 'containers', 'vms', 'worktrees', 'history'] as const
export type Tab = (typeof tabs)[number]
export const filters = reactive({
  projects: store.get('projects-text') || '',
  containers: '',
  containerStatus: '',
  vms: '',
  previews: store.get('filter-text') || '',
  mode: store.get('filter-mode') || '',
  status: store.get('filter-status') || '',
  history: store.get('history-text') || '',
  outcome: store.get('history-kind') || '',
})
watch(filters, () => {
  for (const [key, value] of Object.entries({
    'projects-text': filters.projects,
    'filter-text': filters.previews,
    'filter-mode': filters.mode,
    'filter-status': filters.status,
    'history-text': filters.history,
    'history-kind': filters.outcome,
  }))
    store.set(key, value)
})
export const state = reactive({
  tab: 'projects' as Tab,
  dialog: '',
  error: '',
  dialogError: '',
  pending: false,
  session: { signed_in: false, password_set: false } as Session,
  overview: null as Overview | null,
  projects: null as { projects: Project[]; directory: string; warnings: string[] } | null,
  containers: null as { containers: Container[]; warnings?: string[] } | null,
  vms: null as { vms: VM[]; warnings: string[] } | null,
  history: [] as HistoryEvent[],
  resources: null as Resources | null,
  errors: {} as Record<string, string>,
  busy: {} as Record<string, boolean>,
  updated: {} as Record<string, string>,
  action: null as ActionRequest | null,
  actionName: '',
  details: null as ContainerDetails | null,
  logTarget: {} as LogTarget,
  logTitle: '',
  logLines: [] as string[],
  follow: false,
  hideHealth: true,
  streaming: false,
  job: { id: '', title: '', output: '', status: '' },
  pairing: null as Pairing | null,
})
export const currentSample = computed(() => state.resources?.history?.at(-1))
export const confirmationInfo = computed(() => (state.action ? confirmation(state.action) : null))
export const visibleLogs = computed(() =>
  (state.hideHealth
    ? state.logLines.filter((line) => !/"(GET|HEAD) \/health\/?\S* HTTP|healthcheck/i.test(line))
    : state.logLines
  ).join('\n')
)
let stream: AbortController | undefined
let dialogGeneration = 0
watch(
  () => state.dialog,
  (_next, previous) => {
    dialogGeneration++
    state.dialogError = ''
    if (previous === 'logs') {
      stream?.abort()
      stream = undefined
      state.streaming = false
      state.logLines = []
    }
    if (previous === 'pair') state.pairing = null
    if (previous === 'details') state.details = null
    if (previous === 'job') state.job.id = ''
    if (_next === 'system') void loadResources()
  },
  { flush: 'sync' }
)

export async function loadSession() {
  try {
    state.session = await request<Session>('session')
    state.errors.session = ''
  } catch (error) {
    state.session = { signed_in: false, password_set: false }
    state.errors.session = `Could not load sign-in status: ${errorMessage(error)}`
  }
}
async function loadInventory(key: 'overview' | 'projects' | 'containers' | 'vms' | 'history') {
  if (state.busy[key]) return
  state.busy[key] = true
  try {
    // The independent endpoints may succeed while Docker or another provider is down.
    const data = await request<unknown>(key, { signal: AbortSignal.timeout(20000) })
    if (key === 'history') state.history = (data as { events: HistoryEvent[] | null }).events || []
    else if (key === 'overview') state.overview = data as Overview
    else if (key === 'projects') state.projects = data as NonNullable<typeof state.projects>
    else if (key === 'containers') state.containers = data as NonNullable<typeof state.containers>
    else state.vms = data as NonNullable<typeof state.vms>
    state.errors[key] = ''
    state.updated[key] = new Date().toLocaleTimeString()
  } catch (error) {
    state.errors[key] = `Could not refresh ${key}: ${errorMessage(
      error
    )}. Previous results are retained. Use Refresh to retry.`
  } finally {
    state.busy[key] = false
  }
}
export async function refresh() {
  await Promise.all([
    loadInventory('overview'),
    loadInventory('history'),
    loadInventory('containers'),
    loadInventory('vms'),
    ...(state.tab === 'projects' ? [loadInventory('projects')] : []),
  ])
}
export async function loadResources() {
  if (state.busy.resources) return
  state.busy.resources = true
  try {
    state.resources = await request<Resources>(
      `resources${state.dialog === 'system' ? '?full=1' : ''}`,
      { signal: AbortSignal.timeout(15000) }
    )
    state.errors.resources = ''
  } catch (error) {
    state.errors.resources = `Could not refresh system metrics: ${errorMessage(error)}`
  } finally {
    state.busy.resources = false
  }
}
export function startPolling() {
  void loadSession()
  void refresh()
  void loadResources()
  const inventory = window.setInterval(() => {
    if (
      !document.hidden &&
      !state.dialog &&
      !document.querySelector('[data-state="open"][role="menu"]') &&
      !document.activeElement?.closest('[data-inventory]')
    )
      void refresh()
  }, 10000)
  const resources = window.setInterval(() => {
    if (!document.hidden) void loadResources()
  }, 5000)
  return () => {
    clearInterval(inventory)
    clearInterval(resources)
    stream?.abort()
    state.job.id = ''
    dialogGeneration++
  }
}
export function openDialog(dialog: string) {
  state.dialog = dialog
  state.dialogError = ''
}
export async function signIn(password: string) {
  if (state.pending) return
  state.pending = true
  try {
    await post('login', { password })
    await loadSession()
    state.dialog = ''
    await refresh()
  } catch (error) {
    state.dialogError = errorMessage(error)
  } finally {
    state.pending = false
  }
}
export async function signOut() {
  if (state.pending) return
  state.pending = true
  try {
    await post('logout')
    state.dialog = ''
    await loadSession()
  } catch (error) {
    state.error = errorMessage(error)
  } finally {
    state.pending = false
  }
}
export async function act(action: ActionRequest, name = '') {
  if (state.pending) return
  if (!state.session.signed_in) {
    openDialog('signin')
    return
  }
  state.action = { ...action }
  state.actionName =
    name || action.preview || action.vm || action.container || action.worktree || action.repo || ''
  if (confirmation(action)) openDialog('action')
  else await runAction(action)
}
export async function runAction(action: ActionRequest) {
  if (state.pending) return
  state.pending = true
  state.dialogError = ''
  state.error = ''
  try {
    const job = await post<{ id: string; title: string }>('action', action)
    openDialog('job')
    state.job = { ...job, output: '', status: 'running' }
    void followJob(job.id)
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      await loadSession()
      openDialog('signin')
      state.dialogError = 'Your session expired. Sign in, then choose the action again.'
    } else if (state.dialog === 'action') state.dialogError = errorMessage(error)
    else state.error = `${actionLabels[action.action] || action.action}: ${errorMessage(error)}`
  } finally {
    state.pending = false
  }
}
async function followJob(id: string) {
  let since = 0
  while (state.job.id === id && state.dialog === 'job') {
    try {
      const job = await request<{ output: string; next: number; done: boolean; ok: boolean }>(
        `job?id=${encodeURIComponent(id)}&since=${since}`,
        { signal: AbortSignal.timeout(15000) }
      )
      if (state.job.id !== id || state.dialog !== 'job') return
      state.job.output += job.output
      since = job.next
      if (job.done) {
        state.job.status = job.ok ? 'done' : 'failed'
        await refresh()
        return
      }
    } catch (error) {
      if (state.job.id === id) state.job.status = errorMessage(error)
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 700))
  }
}
export async function showDetails(target: LogTarget, title: string) {
  openDialog('details')
  const generation = ++dialogGeneration
  state.details = null
  state.logTitle = title
  try {
    const data = await request<ContainerDetails>(`container?${targetQuery(target)}`)
    if (generation === dialogGeneration) state.details = data
  } catch (error) {
    if (generation === dialogGeneration) state.dialogError = errorMessage(error)
  }
}
export function showLogs(target: LogTarget, title: string) {
  openDialog('logs')
  state.logTarget = target
  state.logTitle = title
  void loadLogs()
}
export async function loadLogs() {
  stream?.abort()
  const controller = new AbortController()
  stream = controller
  state.logLines = []
  state.dialogError = ''
  state.streaming = state.follow
  try {
    const response = await fetch(
      `/api/logs?${targetQuery(state.logTarget)}${state.follow ? '&follow=1' : ''}`,
      { signal: controller.signal, credentials: 'same-origin' }
    )
    if (!response.ok) throw new Error(await response.text())
    if (!response.body) throw new Error('Log streaming is unavailable in this browser.')
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let carry = ''
    for (;;) {
      const { value, done } = await reader.read()
      if (stream !== controller || state.dialog !== 'logs') return
      const text = carry + (done ? decoder.decode() : decoder.decode(value, { stream: true }))
      const lines = text.split('\n')
      carry = lines.pop() || ''
      state.logLines.push(...lines)
      if (done && carry) state.logLines.push(carry)
      if (state.logLines.length > 5000) state.logLines.splice(0, state.logLines.length - 5000)
      if (done) break
    }
  } catch (error) {
    if (!controller.signal.aborted && stream === controller) state.dialogError = errorMessage(error)
  } finally {
    if (stream === controller) {
      state.streaming = false
      stream = undefined
    }
  }
}
export async function pairDevice() {
  openDialog('pair')
  const generation = ++dialogGeneration
  state.pairing = null
  try {
    const pairing = await post<Pairing>('pair')
    if (generation === dialogGeneration) state.pairing = pairing
  } catch (error) {
    if (generation === dialogGeneration) {
      if (error instanceof APIError && error.status === 401) {
        await loadSession()
        openDialog('signin')
      } else state.dialogError = errorMessage(error)
    }
  }
}
