// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
const request = vi.hoisted(() => vi.fn())
vi.mock('../src/secrets', async original => ({ ...await original<typeof import('../src/secrets')>(), secretRequest: request }))
vi.mock('../src/dashboard', () => ({ state: { session: { signed_in: true } } }))
import ProjectSecrets from '../src/components/ProjectSecrets.vue'
const writeText = vi.fn(async (_value: string) => {})
beforeEach(() => {
  writeText.mockClear()
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  request.mockReset().mockImplementation(async q => ({
    context: { project: 'p', worktree: 'w', root: q.root }, resources: [],
    records: ['APP_USERNAME', 'APP_PASSWORD'].map(name => ({ name, resource: 'w', scope: 'worktree', effective: true, inherited: false, version: 1, deleted: false, updated: '2026-11-09T00:00:00Z' })),
    ...(q.operation === 'get' ? { value: q.key === 'APP_USERNAME' ? 'demo-user' : 'synthetic-password' } : {}),
  }))
})
it('reveals only in the matching row and hides when another row is revealed', async () => {
  const view = mount(ProjectSecrets, { props: { root: '/repo', defaultScope: 'worktree' } })
  await flushPromises()
  const rows = view.findAll('tbody tr')
  await rows[1]!.findAll('button').find(b => b.text() === 'Reveal')!.trigger('click')
  await flushPromises()
  expect(rows[1]!.get('output').text()).toBe('synthetic-password')
  expect(rows[0]!.find('output').exists()).toBe(false)
  await rows[0]!.findAll('button').find(b => b.text() === 'Reveal')!.trigger('click')
  await flushPromises()
  expect(rows[0]!.get('output').text()).toBe('demo-user')
  expect(rows[1]!.find('output').exists()).toBe(false)
  view.unmount()
})
it('copies username and password through labeled icons without revealing either value', async () => {
  const view = mount(ProjectSecrets, { props: { root: '/repo' } })
  await flushPromises()
  for (const [kind, key, value] of [['username', 'APP_USERNAME', 'demo-user'], ['password', 'APP_PASSWORD', 'synthetic-password']]) {
    const button = view.get(`button[aria-label="Copy ${kind}: ${key}"]`)
    expect(button.find('svg').exists()).toBe(true)
    await button.trigger('click'); await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith(value)
    expect(view.text()).not.toContain(value)
    expect(view.find('output').exists()).toBe(false)
  }
  view.unmount()
})
it('does not copy a delayed response after changing context', async () => {
  const view = mount(ProjectSecrets, { props: { root: '/repo' } })
  await flushPromises()
  let finish!: (value: unknown) => void
  request.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  await view.get('button[aria-label="Copy password: APP_PASSWORD"]').trigger('click')
  await view.setProps({ root: '/another-repo' }); await flushPromises()
  finish({ value: 'stale-password' }); await flushPromises()
  expect(writeText).not.toHaveBeenCalled()
  expect(view.find('output').exists()).toBe(false)
  view.unmount()
})
it('identifies the selected branch and checkout and switches to the chosen worktree', async () => {
  const view = mount(ProjectSecrets, { props: { root: '/repo', defaultScope: 'worktree', checkouts: [{ path: '/repo', branch: 'main' }, { path: '/repo-feature', branch: 'feature/login' }] } })
  await flushPromises()
  expect(view.get('[aria-label="Selected secret context"]').text()).toContain('main')
  expect(view.get('[aria-label="Selected secret context"]').text()).toContain('/repo')
  await view.get('select[aria-label="Checkout"]').setValue('/repo-feature')
  await flushPromises()
  expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ operation: 'list', root: '/repo-feature', scope: 'worktree' }))
  expect(view.get('[aria-label="Selected secret context"]').text()).toContain('feature/login')
})
it('creates a paired login and copies/reveals only the selected field', async () => {
  request.mockImplementation(async q => ({
    context: { project: 'p', worktree: 'w', root: q.root }, resources: [],
    records: [{ name: 'dev-admin', type: 'login', resource: 'w', scope: 'worktree', effective: true, inherited: false, version: 1, deleted: false, updated: '2026-11-09T00:00:00Z' }],
    ...(q.operation === 'get' ? { value: q.field === 'username' ? 'paired-user' : 'paired-password' } : {}),
  }))
  const view = mount(ProjectSecrets, { props: { root: '/repo', defaultScope: 'worktree' } })
  await flushPromises()
  for (const [field, value] of [['username', 'paired-user'], ['password', 'paired-password']]) {
    await view.get(`button[aria-label="Copy ${field}: dev-admin"]`).trigger('click'); await flushPromises()
    expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ operation: 'get', field, reveal: true }))
    expect(writeText).toHaveBeenLastCalledWith(value)
    expect(view.find('output').exists()).toBe(false)
  }
  await view.get('button[aria-label="Reveal username: dev-admin"]').trigger('click'); await flushPromises()
  expect(view.get('output').text()).toBe('paired-user')
  expect(view.text()).not.toContain('paired-password')
  await view.get('button[aria-label="Reveal password: dev-admin"]').trigger('click'); await flushPromises()
  expect(view.get('output').text()).toBe('paired-password')
  expect(view.text()).not.toContain('paired-user')
  await view.get('select[aria-label="Entry type"]').setValue('login')
  await view.get('input[aria-label="Name"]').setValue('new-login')
  await view.get('input[aria-label="Username"]').setValue('new-user')
  await view.get('input[aria-label="Password"]').setValue('new-password')
  await view.get('form').trigger('submit'); await flushPromises()
  expect(request).toHaveBeenCalledWith(expect.objectContaining({ operation: 'set', type: 'login', username: 'new-user', password: 'new-password', key: 'new-login', scope: 'worktree' }))
  expect((view.get('input[aria-label="Username"]').element as HTMLInputElement).value).toBe('')
  expect((view.get('input[aria-label="Password"]').element as HTMLInputElement).value).toBe('')
  view.unmount()
})
