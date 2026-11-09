import { ref } from 'vue'
import { post } from './api'
export interface SecretRecord { type?: 'value' | 'login'; username_env?: string; password_env?: string; name: string; resource: string; scope: string; inherited: boolean; effective: boolean; version: number; updated: string; deleted: boolean; deadline?: string }
export interface SecretResource { id: string; kind: string; name: string; state: string; path?: string; deadline?: string }
export interface CleanupConfig { automatic: boolean; branch: string; worktree: string; project: string; resume_after?: string }
export interface BackupConfig { recipient: string; directory: string; daily: boolean; retain: number }
export interface SecretReply {
  counts?: Record<string, number>
  context: { project: string; branch?: string; worktree?: string; root: string }
  records: SecretRecord[] | null; resources?: SecretResource[]; value?: string
  confirmation?: string; message?: string; cleanup?: CleanupConfig; history?: unknown
  management?: { agents?: Record<string, string>; status?: { state: string; version?: string }; config?: { provider: string; image: string; backup: BackupConfig }; url?: string; maintenance?: string; integrations?: { service: string; key: string; source: string }[] }
}
export type SecretRequest = { operation: string; [key: string]: unknown }
export async function secretRequest(request: SecretRequest) {
  try { return await post<SecretReply>('secrets', request) }
  catch (error) {
    if (error instanceof Error) { try { const body = JSON.parse(error.message); if (typeof body.error === 'string') throw new Error(body.error) } catch (parsed) { if (parsed instanceof Error && !(parsed instanceof SyntaxError)) throw parsed } }
    throw error
  }
}
export function transientSecret() {
  const value = ref('')
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  function clear() { generation++; value.value = ''; if (timer) clearTimeout(timer); timer = undefined }
  async function reveal(load: () => Promise<string>) {
    clear(); const current = generation; const next = await load()
    if (current !== generation) return
    value.value = next; timer = setTimeout(clear, 30_000)
  }
  return { value, clear, reveal }
}
export function validRetention(input: string) {
  if (input === 'never') return true
  if (/^[1-9]\d*d$/.test(input)) return Number(input.slice(0, -1)) <= 36500
  const parts = [...input.matchAll(/(\d+(?:\.\d+)?)(h|m|s)/g)]
  if (!parts.length || parts.map(p => p[0]).join('') !== input) return false
  const seconds = parts.reduce((sum, p) => sum + Number(p[1]) * ({ h: 3600, m: 60, s: 1 }[p[2]!] || 0), 0)
  return seconds >= 3600 && seconds <= 36500 * 86400
}
