export const up = (status: string) =>
  ['healthy', 'running', 'unhealthy', 'starting'].includes(status)
export const short = (value?: string | null) => (value || '').slice(0, 10)
export const home = (value: string) => value.replace(/^\/home\/[^/]+/, '~')
export const bytes = (value?: number | null) =>
  value == null
    ? 'Unavailable'
    : value >= 2 ** 30
    ? `${(value / 2 ** 30).toFixed(1)} GiB`
    : `${Math.round(value / 2 ** 20)} MiB`
export const percent = (used: number, total: number) => (total ? (used / total) * 100 : 0)
export function ago(iso?: string | null) {
  if (!iso || !Number.isFinite(Date.parse(iso))) return '—'
  const seconds = Math.max(0, (Date.now() - Date.parse(iso)) / 1000)
  return seconds < 90
    ? `${Math.round(seconds)}s`
    : seconds < 5400
    ? `${Math.round(seconds / 60)}m`
    : seconds < 172800
    ? `${Math.round(seconds / 3600)}h`
    : `${Math.round(seconds / 86400)}d`
}
export function wordsMatch(query: string, values: unknown[]) {
  const hay = values.join(' ').toLowerCase()
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((word) => hay.includes(word))
}
// Host metadata never gets to choose a script or external protocol for an app link.
export function safeURL(value?: string) {
  try {
    const url = new URL(value || '')
    return ['http:', 'https:'].includes(url.protocol) ? url.href : undefined
  } catch {
    return undefined
  }
}
export const store = {
  get(key: string) {
    try {
      return localStorage.getItem(`devkit.${key}`)
    } catch {
      return null
    }
  },
  set(key: string, value: string) {
    try {
      localStorage.setItem(`devkit.${key}`, value)
    } catch {
      /* Private/storage-disabled browsing still works. */
    }
  },
}
export const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : String(error)
export async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    const area = document.createElement('textarea')
    area.value = text
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.append(area)
    area.select()
    const copied = document.execCommand('copy')
    area.remove()
    if (!copied) throw new Error('Copy is unavailable. Select and copy the text manually.')
  }
}
export const actionLabels: Record<string, string> = {
  'project-create': 'Create Project',
  'document-save': 'Save document',
  up: 'Launch containers',
  live: 'Start live preview',
  review: 'Review HEAD',
  start: 'Start',
  stop: 'Stop',
  destroy: 'Destroy…',
  share: 'Share publicly…',
  unshare: 'Stop sharing',
  gc: 'Clean up…',
  'git-fetch': 'Fetch origin',
  'git-sync': 'Sync from origin',
  'git-push': 'Push to origin',
  'vm-start': 'Start',
  'vm-stop': 'Turn off',
  'vm-restart': 'Restart',
  'container-start': 'Start',
  'container-stop': 'Turn off',
}
export function day(iso: string) {
  const date = new Date(iso)
  const today = new Date()
  const days = Math.round(
    (new Date(today.toDateString()).getTime() - new Date(date.toDateString()).getTime()) / 86400000
  )
  return days === 0 ? 'Today' : days === 1 ? 'Yesterday' : date.toLocaleDateString()
}

const historyVerbs: Record<string, string> = {
  'project-create': 'Created project',
  'document-save': 'Saved document',
  live: 'Live preview',
  review: 'Review',
  publish: 'Published',
  start: 'Started',
  stop: 'Stopped',
  destroy: 'Destroyed',
  share: 'Shared publicly',
  extend: 'Share extended',
  unshare: 'Stopped sharing',
  expire: 'Share expired',
  sweep: 'Leftover removed',
  gc: 'Clean-up',
  'unshare --expired': 'Expired shares',
}
export const historyVerb = (event: { action: string; ok: boolean }) =>
  event.ok ? historyVerbs[event.action] || event.action : `${event.action} failed`
