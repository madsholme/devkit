import { test, expect } from '@playwright/test'

test('save metadata, reveal explicitly and clear when leaving the secret context', async ({ page, context }) => {
  await context.addCookies([{ name: 'fixture', value: 'signed', url: 'http://127.0.0.1:4178' }])
  let saved = false
  let reveals = 0
  await page.route('**/api/secrets', async route => {
    const q = route.request().postDataJSON()
    const result: Record<string, unknown> = { context: { project: 'project1', branch: 'branch1', worktree: 'tree1', root: q.root }, resources: [], records: [] }
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
  await panel.getByRole('button', { name: 'Reveal', exact: true }).click()
  await expect(panel.locator('output')).toHaveText('synthetic-secret-value')
  await page.getByRole('navigation', { name: 'Project sections' }).getByRole('link', { name: 'Overview' }).click()
  await page.getByRole('navigation', { name: 'Project sections' }).getByRole('link', { name: 'Secrets', exact: true }).click()
  await expect(panel.locator('output')).toHaveCount(0)
  await panel.getByRole('button', { name: 'Reveal', exact: true }).click()
  await expect(panel.locator('output')).toBeVisible()
  await page.locator('.site-header').getByRole('link', { name: /^Projects \(/ }).click()
  await page.goBack()
  await expect(panel.getByRole('cell', { name: 'PASSWORD', exact: true })).toBeVisible()
  await expect(panel.locator('output')).toHaveCount(0)
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click()
  await expect(panel.locator('output')).toHaveCount(0)
  await expect(panel.getByText('Sign in to view or manage secrets.')).toBeVisible()
})
