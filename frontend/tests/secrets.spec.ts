import { test, expect } from '@playwright/test'

test('save metadata, reveal explicitly and clear when leaving the secret context', async ({ page, context }) => {
  await context.addCookies([{ name: 'fixture', value: 'signed', url: 'http://127.0.0.1:4178' }])
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  let saved = false
  let reveals = 0
  await page.route('**/api/secrets', async route => {
    const q = route.request().postDataJSON()
    const result: Record<string, unknown> = { context: { project: 'project1', branch: 'branch1', worktree: 'tree1', root: q.root }, resources: [{ id: 'branch1', kind: 'branch', name: 'main', state: 'active' }], counts: { project1: saved ? 1 : 0 }, records: [] }
    if (q.operation === 'generate') saved = true
    if (q.operation === 'list' && saved) result.records = [{ name: 'PASSWORD', resource: 'project1', scope: 'project', inherited: false, effective: true, version: 1, updated: '2026-11-08T00:00:00Z', deleted: false }]
    if (q.operation === 'get' && q.reveal) { reveals++; result.value = 'synthetic-secret-value' }
    await route.fulfill({ json: result })
  })
  const project = '/#/projects/' + encodeURIComponent('/home/demo/Work/atlas')
  await page.goto(project + '/secrets')
  const panel = page.getByRole('region', { name: 'Project secrets' })
  await panel.getByLabel('Name', { exact: true }).fill('PASSWORD')
  await panel.getByRole('button', { name: 'Generate password' }).click()
  await expect(panel.getByRole('cell', { name: 'PASSWORD', exact: true })).toBeVisible()
  await expect(page.getByText('synthetic-secret-value', { exact: true })).toHaveCount(0)
  expect(reveals).toBe(0)
  await panel.getByLabel('Scope', { exact: true }).selectOption('worktree')
  await expect(panel.getByLabel('Selected secret context')).toContainText('main')
  await panel.getByRole('button', { name: 'Copy password: PASSWORD', exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('synthetic-secret-value')
  await expect(panel.locator('output')).toHaveCount(0)
  await panel.getByRole('button', { name: 'Reveal', exact: true }).click()
  await expect(panel.getByRole('row').filter({ has: page.getByRole('cell', { name: /^PASSWORD/ }) }).locator('output')).toHaveText('synthetic-secret-value')
  await page.screenshot({ path: '/tmp/devkit-secret-row-desktop.png', fullPage: true })
  await page.getByRole('navigation', { name: 'Project sections' }).getByRole('link', { name: 'Overview' }).click()
  await page.getByRole('navigation', { name: 'Project sections' }).getByRole('link', { name: /^Secrets(?: |$)/ }).click()
  await expect(panel.locator('output')).toHaveCount(0)
  await panel.getByRole('button', { name: 'Reveal', exact: true }).click()
  await expect(panel.locator('output')).toBeVisible()
  await page.locator('.site-header').getByRole('link', { name: /^Projects \(/ }).click()
  await page.goBack()
  await expect(panel.getByRole('cell', { name: 'PASSWORD', exact: true })).toBeVisible()
  await expect(panel.locator('output')).toHaveCount(0)
  await page.setViewportSize({ width: 390, height: 844 })
  await panel.getByRole('button', { name: 'Reveal', exact: true }).click()
  await expect(panel.locator('output')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: '/tmp/devkit-secret-row-mobile.png', fullPage: true })
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click()
  await expect(panel.locator('output')).toHaveCount(0)
  await expect(panel.getByText('Sign in to view or manage secrets.')).toBeVisible()
})

test('project tabs show document and private secret totals and update after saving', async ({ page, context }) => {
  await context.addCookies([{ name: 'fixture', value: 'signed', url: 'http://127.0.0.1:4178' }])
  let saved = false
  await page.route('**/api/documents?*', route => route.fulfill({ json: { files: [{ path: 'README.md', editable: false }, { path: 'docs/setup.md', editable: true }], warnings: [] } }))
  await page.route('**/api/secrets', async route => {
    const q = route.request().postDataJSON()
    if (q.operation === 'generate') saved = true
    await route.fulfill({ json: {
      context: { project: 'p', branch: 'b', worktree: 'w', root: q.root },
      resources: [{ id: 'p', kind: 'project', name: 'atlas', state: 'active' }, { id: 'b', kind: 'branch', name: 'main', state: 'active' }, { id: 'w', kind: 'worktree', name: 'atlas', path: '/home/demo/Work/atlas', state: 'active' }],
      counts: { p: saved ? 2 : 1, b: 2, w: 3 }, records: [],
    } })
  })
  await page.goto('/#/projects/' + encodeURIComponent('/home/demo/Work/atlas') + '/overview')
  const nav = page.getByRole('navigation', { name: 'Project sections' })
  await expect(nav.getByRole('link', { name: 'Secrets 6', exact: true })).toBeVisible()
  await expect(nav.getByRole('link', { name: 'Documents 2', exact: true })).toBeVisible()
  await nav.getByRole('link', { name: 'Secrets 6', exact: true }).click()
  const panel = page.getByRole('region', { name: 'Project secrets' })
  await panel.getByLabel('Name', { exact: true }).fill('NEW_PASSWORD')
  await panel.getByRole('button', { name: 'Generate password' }).click()
  await expect(nav.getByRole('link', { name: 'Secrets 7', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click()
  await expect(nav.getByRole('link', { name: 'Secrets', exact: true })).toBeVisible()
})

test('paired logins keep fields together and copy each without revealing the other', async ({ page, context }) => {
  await context.addCookies([{ name: 'fixture', value: 'signed', url: 'http://127.0.0.1:4178' }])
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  let login: Record<string, unknown> | undefined
  await page.route('**/api/secrets', async route => {
    const q = route.request().postDataJSON()
    if (q.operation === 'generate') { expect(q.type).toBe('login'); expect(q.username).toBe('demo-admin'); login = { ...q, password: 'synthetic-login-password' } }
    const record = { name: 'dev-admin', type: 'login', resource: 'w', scope: 'worktree', effective: true, inherited: false, deleted: false, version: 1, updated: '2026-11-09T00:00:00Z' }
    await route.fulfill({ json: { context: { project: 'p', branch: 'b', worktree: 'w', root: q.root }, counts: { p: 0, w: login ? 1 : 0 }, resources: [{ id: 'b', kind: 'branch', name: 'main', state: 'active' }], records: login ? [record] : [], ...(q.operation === 'get' ? { value: login?.[q.field] } : {}) } })
  })
  await page.goto('/#/projects/' + encodeURIComponent('/home/demo/Work/atlas') + '/secrets')
  const panel = page.getByRole('region', { name: 'Project secrets' })
  await panel.getByLabel('Scope', { exact: true }).selectOption('worktree')
  await panel.getByLabel('Entry type', { exact: true }).selectOption('login')
  await panel.getByLabel('Name', { exact: true }).fill('dev-admin')
  await panel.getByLabel('Username', { exact: true }).fill('demo-admin')
  await panel.getByRole('button', { name: 'Generate password', exact: true }).click()
  await expect(panel.getByLabel('Username', { exact: true })).toHaveValue('')
  const row = panel.getByRole('row').filter({ has: page.locator('code', { hasText: 'dev-admin' }) })
  await expect(row).toHaveCount(1)
  for (const [field, value] of [['username', 'demo-admin'], ['password', 'synthetic-login-password']]) {
    await row.getByRole('button', { name: `Copy ${field}: dev-admin`, exact: true }).click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(value)
    await expect(row.locator('output')).toHaveCount(0)
  }
  await row.getByRole('button', { name: 'Reveal username: dev-admin', exact: true }).click()
  await expect(row.locator('output')).toHaveText('demo-admin')
  await expect(row).not.toContainText('synthetic-login-password')
  await row.getByRole('button', { name: 'Reveal password: dev-admin', exact: true }).click()
  await expect(row.locator('output')).toHaveText('synthetic-login-password')
  await expect(row).not.toContainText('demo-admin')
  await page.screenshot({ path: '/tmp/devkit-login-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: '/tmp/devkit-login-mobile.png', fullPage: true })
  await panel.getByLabel('Scope', { exact: true }).selectOption('project')
  await expect(panel.locator('output')).toHaveCount(0)
})
