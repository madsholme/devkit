// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
const request = vi.hoisted(() => vi.fn(async (q: Record<string, unknown>) => {
  if (q.operation === 'vault-status') return { management: { status: { state: 'ready' }, config: { image: 'openbao/openbao:2.7.2', backup: {} } } }
  if (q.operation === 'cleanup-config') return q.cleanup ? { confirmation: 'review-id', message: 'Review cleanup change' } : { cleanup: { automatic: true, branch: '7d', worktree: '7d', project: '30d' } }
  return { management: { integrations: [] }, resources: [] }
}))
vi.mock('../src/secrets', async (original) => ({ ...await original<typeof import('../src/secrets')>(), secretRequest: request }))
vi.mock('../src/dashboard', () => ({ state: { session: { signed_in: true } } }))
import SecretsSettingsView from '../src/views/SecretsSettingsView.vue'
it('previews disabling cleanup and keeps never as an explicit choice', async () => {
  const view = mount(SecretsSettingsView)
  await flushPromises()
  await view.get('input[type=checkbox][name=automatic]').setValue(false)
  await view.get('input[name=branch-retention]').setValue('never')
  await view.get('form[aria-label="Cleanup settings"]').trigger('submit')
  await flushPromises()
  expect(request).toHaveBeenCalledWith(expect.objectContaining({ operation: 'cleanup-config', cleanup: expect.objectContaining({ automatic: false, branch: 'never' }) }))
  expect(view.text()).toContain('Review cleanup change')
  expect(request.mock.calls.some(([q]) => q.confirm)).toBe(false)
  view.unmount()
})
