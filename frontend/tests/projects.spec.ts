import { test, expect } from '@playwright/test'

test('project directory, local resource tabs and worktree drill-down preserve global navigation', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto('/#/projects')
  await page.getByLabel('Filter projects').fill('atlas')
  await page.getByRole('link', { name: 'atlas', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'atlas', exact: true })).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'Project information', exact: true })
  ).toBeVisible()
  const projectNav = page.getByRole('navigation', { name: 'Project sections' })
  await projectNav.getByRole('link', { name: /Worktrees/ }).click()
  await page.getByRole('link', { name: 'feature/search', exact: true }).click()
  const worktreeNav = page.getByRole('navigation', { name: 'Worktree sections' })
  await worktreeNav.getByRole('link', { name: /Containers/ }).click()
  await expect(page.getByRole('row').filter({ hasText: 'atlas-search-web' })).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'atlas-db' })).toHaveCount(0)
  await worktreeNav.getByRole('link', { name: /Virtual machines/ }).click()
  await expect(page.getByText('atlas-search-lab', { exact: true })).toBeVisible()
  await expect(page.getByText('atlas-lab', { exact: true })).toHaveCount(0)
  await expect(page.getByText('atlas-shared', { exact: true })).toHaveCount(0)
  await page.reload()
  await expect(page.getByText('atlas-search-lab', { exact: true })).toBeVisible()
  await projectNav.getByRole('link', { name: /Containers/ }).click()
  for (const name of ['atlas-web', 'atlas-db', 'atlas-search-web'])
    await expect(page.getByText(name, { exact: true })).toBeVisible()
  const featureRow = page.getByRole('row').filter({ hasText: 'atlas-search-web' })
  await expect(featureRow.getByRole('link', { name: 'feature/search', exact: true })).toBeVisible()
  await featureRow.getByRole('link', { name: 'feature/search', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Checkout information', exact: true })
  ).toBeVisible()
  await page.goBack()
  await expect(page.getByLabel('Filter project containers')).toBeVisible()
  await projectNav.getByRole('link', { name: /Virtual machines/ }).click()
  const shared = page.getByRole('row').filter({ hasText: 'atlas-shared' })
  await expect(shared).toContainText('Unassigned worktree')
  await expect(shared).toContainText('Mapped by multiple worktrees')
  await expect(shared.getByRole('link')).toHaveCount(0)
  await page.getByRole('link', { name: 'Back to Projects', exact: true }).click()
  await expect(page.getByLabel('Filter projects')).toHaveValue('atlas')
  await page
    .locator('.site-header')
    .getByRole('link', { name: /^Containers \(/ })
    .click()
  await expect(page.getByRole('heading', { name: 'Containers', exact: true })).toBeVisible()
  await expect(page.getByText('standalone-worker', { exact: true })).toBeVisible()
  expect(errors).toEqual([])
})

test('deep links handle removed projects and worktrees, and inventories stay independent', async ({
  page,
}) => {
  const url = '/#/projects/' + encodeURIComponent('/home/demo/Work/atlas')
  await page.goto(url + '/containers')
  await expect(page.getByText('atlas-search-web', { exact: true })).toBeVisible()
  await page.route('**/api/containers', (route) =>
    route.fulfill({ status: 503, body: 'Docker unavailable' })
  )
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'Docker unavailable' })).toBeVisible()
  await expect(page.getByText('atlas-search-web', { exact: true })).toBeVisible()
  await page
    .getByRole('navigation', { name: 'Project sections' })
    .getByRole('link', { name: /Virtual machines/ })
    .click()
  await expect(page.getByText('atlas-search-lab', { exact: true })).toBeVisible()
  await page.goto(url + '/worktrees?tree=' + encodeURIComponent('/gone'))
  await expect(page.getByRole('heading', { name: 'Worktree not found', exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'All worktrees', exact: true }).click()
  await expect(page.getByRole('link', { name: 'feature/search', exact: true })).toBeVisible()
  await page.goto('/#/projects/' + encodeURIComponent('/gone') + '/overview')
  await expect(page.getByRole('heading', { name: 'Project not found', exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'Back to Projects', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Projects', exact: true })).toBeVisible()
})

test('project detail and worktree tabs fit mobile and support keyboard navigation', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/#/projects/' + encodeURIComponent('/home/demo/Work/atlas') + '/overview')
  await expect(
    page.getByRole('heading', { name: 'Project information', exact: true })
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  const nav = page.getByRole('navigation', { name: 'Project sections' })
  await nav.getByRole('link', { name: /Containers/ }).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByText('atlas-db', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await nav.getByRole('link', { name: /Worktrees/ }).click()
  await page.getByRole('link', { name: 'feature/search', exact: true }).click()
  await page
    .getByRole('navigation', { name: 'Worktree sections' })
    .getByRole('link', { name: /Virtual machines/ })
    .click()
  await expect(page.getByText('atlas-search-lab', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('actions in a selected worktree retain exact checkout, container and VM targets', async ({
  page,
}) => {
  const actions: Record<string, unknown>[] = []
  page.on('request', (req) => {
    if (req.url().endsWith('/api/action')) actions.push(req.postDataJSON())
  })
  await page.goto(
    '/#/projects/' +
      encodeURIComponent('/home/demo/Work/atlas') +
      '/worktrees?tree=' +
      encodeURIComponent('/home/demo/Work/atlas-review')
  )
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByLabel('Dashboard password').fill('demo-password')
  await page.getByRole('dialog').getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Fetch origin', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Completed synthetic action')
  expect(actions[0]).toMatchObject({
    action: 'git-fetch',
    worktree: '/home/demo/Work/atlas-review',
  })
  await page.keyboard.press('Escape')
  const tabs = page.getByRole('navigation', { name: 'Worktree sections' })
  await tabs.getByRole('link', { name: /Containers/ }).click()
  await page.getByRole('button', { name: 'Turn off', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Turn off', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Completed synthetic action')
  expect(actions[1]).toMatchObject({
    action: 'container-stop',
    container: 'd'.repeat(64),
    confirm: 'd'.repeat(64),
  })
  await page.keyboard.press('Escape')
  await tabs.getByRole('link', { name: /Virtual machines/ }).click()
  await page.getByRole('button', { name: 'Start', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Completed synthetic action')
  expect(actions[2]).toMatchObject({ action: 'vm-start', vm: 'vm-search' })
})
