import type { ActionRequest, LogTarget } from './types'
export class APIError extends Error {
  constructor(message: string, public status: number) {
    super(message)
  }
}
export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/${path}`, { ...init, credentials: 'same-origin' })
  if (!response.ok)
    throw new APIError((await response.text()) || `HTTP ${response.status}`, response.status)
  return response.json()
}
export function post<T>(path: string, body: unknown = {}) {
  return request<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}
export function targetQuery(target: LogTarget) {
  const params = new URLSearchParams()
  if (target.container) params.set('container', target.container)
  else {
    params.set('preview', target.preview || '')
    if (target.service) params.set('service', target.service)
  }
  return params.toString()
}
export function confirmation(request: ActionRequest) {
  const target = request.vm || request.container || request.preview || ''
  switch (request.action) {
    case 'vm-stop':
    case 'container-stop':
      return {
        title: 'Turn off',
        confirm: target,
        danger: true,
        description: `Requests a graceful shutdown. Services will become unavailable; disks and saved data are retained. ${
          request.action === 'container-stop'
            ? 'Stopping a proxy or dashboard dependency can disconnect this page. '
            : ''
        }A timeout is reported without forcing it off.`,
      }
    case 'destroy':
      return {
        title: 'Destroy',
        confirm: target,
        danger: true,
        description:
          'Removes this preview’s containers, networks and volumes, including its database. The code stays in the worktree.',
      }
    case 'gc':
      return {
        title: 'Clean up',
        confirm: 'remove',
        danger: true,
        description:
          'Destroys previews whose worktree is gone in this repository, including their saved data.',
      }
    case 'share':
      return {
        title: 'Share publicly',
        confirm: undefined,
        danger: false,
        description:
          'Anyone with the link can open it until it expires. The app restarts once to learn its public address.',
      }
    default:
      return null
  }
}
