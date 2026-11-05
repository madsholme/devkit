import { test, expect, type Page } from '@playwright/test'
const signIn = async (page: Page) => {
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByLabel('Dashboard password').fill('demo-password')
  await page.getByRole('dialog').getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
}
test('six views, filters, details, safe anonymous controls and legacy hash links', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto('/#projects')
  await expect(page.getByRole('heading', { name: 'Projects', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Fetch origin' })).toHaveCount(0)
  await page.getByLabel('Filter projects').fill('beacon')
  await expect(page.locator('.project-directory-row')).toHaveCount(1)
  for (const label of ['Projects (2)', 'Previews (2)', 'Worktrees (2)']) {
    await expect(
      page.locator('.site-header').getByRole('link', { name: label, exact: true })
    ).toBeVisible()
  }
  await expect(
    page.locator('.site-header').getByRole('link', { name: 'Containers (5)', exact: true })
  ).toBeVisible()
  await expect(
    page.locator('.site-header').getByRole('link', { name: 'Virtual machines (3)', exact: true })
  ).toBeVisible()
  await page.getByLabel('Filter projects').fill('')
  await page.getByRole('link', { name: /^Containers \(/ }).click()
  await expect(page.getByRole('heading', { name: 'Containers', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Details', exact: true })).toHaveCount(3)
  await page
    .getByRole('row')
    .filter({ hasText: 'atlas-db' })
    .getByRole('button', { name: 'Details', exact: true })
    .click()
  await expect(page.getByRole('dialog')).toContainText('POSTGRES_PASSWORD')
  await page.getByRole('dialog').getByLabel('Filter container details').fill('PGDATA')
  await expect(page.getByRole('dialog')).not.toContainText('POSTGRES_PASSWORD')
  await page.keyboard.press('Escape')
  await page.getByRole('link', { name: /^Virtual machines \(/ }).click()
  await page.getByText('Connect privately', { exact: true }).click()
  await expect(
    page.getByText('remote-viewer spice://127.0.0.1:5931', { exact: true })
  ).toBeVisible()
  await page.getByRole('link', { name: /^Worktrees \(/ }).click()
  await expect(page.getByRole('heading', { name: 'Worktrees', exact: true })).toBeVisible()
  await page.getByRole('link', { name: /^History/ }).click()
  await page.getByLabel('Filter history').fill('failed')
  await expect(page.getByText('Build check failed', { exact: true })).toBeVisible()
  await page.getByRole('link', { name: /^Previews \(/ }).click()
  await page.getByRole('button', { name: 'Show containers for atlas-main' }).click()
  await expect(page.getByRole('button', { name: 'Details', exact: true })).toBeVisible()
  await page.getByLabel('Filter previews').fill('missing')
  await expect(page.getByText('No previews found')).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('Filter previews')).toHaveValue('missing')
  expect(errors).toEqual([])
})
test('sign-in, cancel/confirm exact container and VM targets, preview destroy and share', async ({
  page,
}) => {
  const actions: Record<string, unknown>[] = []
  page.on('request', (req) => {
    if (req.url().endsWith('/api/action')) actions.push(req.postDataJSON())
  })
  await page.goto('/#/containers')
  await expect(
    page.locator('.site-header').getByRole('link', { name: 'Projects (2)', exact: true })
  ).toBeVisible()
  await expect(
    page.locator('.site-header').getByRole('link', { name: 'Virtual machines (3)', exact: true })
  ).toBeVisible()
  await signIn(page)
  const row = page.getByRole('row').filter({ hasText: 'atlas-db' })
  await row.getByRole('button', { name: 'Turn off', exact: true }).click()
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  expect(actions).toHaveLength(0)
  await row.getByRole('button', { name: 'Turn off', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Turn off', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Completed synthetic action')
  expect(actions[0]).toMatchObject({
    action: 'container-stop',
    container: 'a'.repeat(64),
    confirm: 'a'.repeat(64),
  })
  await page.keyboard.press('Escape')
  await page.getByRole('link', { name: /^Virtual machines \(/ }).click()
  await page.getByRole('button', { name: 'Turn off', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Turn off', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Completed synthetic action')
  expect(actions[1]).toMatchObject({
    action: 'vm-stop',
    vm: 'qmp:atlas-lab',
    confirm: 'qmp:atlas-lab',
  })
  await page.keyboard.press('Escape')
  await page.getByRole('link', { name: /^Previews \(/ }).click()
  await page.getByRole('button', { name: 'Actions for atlas-main' }).click()
  await page.getByRole('menuitem', { name: 'Destroy…' }).click()
  await expect(page.getByRole('dialog')).toContainText('including its database')
  await page.getByRole('dialog').getByRole('button', { name: 'Destroy', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Completed synthetic action')
  expect(actions[2]).toMatchObject({
    action: 'destroy',
    preview: 'atlas-main',
    confirm: 'atlas-main',
  })
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Actions for atlas-main' }).click()
  await page.getByRole('menuitem', { name: 'Share publicly…' }).click()
  await page.getByLabel('Hours').fill('6')
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Share publicly', exact: true })
    .click()
  await expect(page.getByRole('dialog')).toContainText('Completed synthetic action')
  expect(actions[3]).toMatchObject({ action: 'share', preview: 'atlas-main', hours: 6, host: '' })
})
test('log follow stops on close, pairing clears, system uses full sampling only while open', async ({
  page,
}) => {
  await page.goto('/#/containers')
  await page
    .getByRole('row')
    .filter({ hasText: 'atlas-db' })
    .getByRole('button', { name: 'Logs', exact: true })
    .click()
  await expect(page.locator('.log-output')).toContainText('application connected')
  await expect(page.locator('.log-output')).not.toContainText('healthcheck')
  await page.getByLabel('Hide health checks', { exact: true }).click()
  await expect(page.locator('.log-output')).toContainText('healthcheck')
  await page.getByLabel('Follow', { exact: true }).click()
  await expect(page.locator('.log-output')).toContainText('stream heartbeat')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const full = page.waitForRequest((r) => r.url().includes('/api/resources?full=1'))
  await page.getByRole('button', { name: 'Open system monitor' }).click()
  await full
  await expect(page.getByRole('dialog')).toContainText('Host processes')
  await page.keyboard.press('Escape')
  await signIn(page)
  await page.getByRole('button', { name: 'Pair device', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Pairing link', exact: true })).toHaveValue(
    /synthetic-only/
  )
  await page.keyboard.press('Escape')
  await expect(page.getByRole('textbox', { name: 'Pairing link', exact: true })).toHaveCount(0)
})
test('independent failure retains data and expired actions require fresh sign-in without replay', async ({
  page,
}) => {
  await page.goto('/#/projects')
  await expect(page.getByRole('link', { name: 'atlas', exact: true })).toBeVisible()
  await page.route('**/api/overview', (route) =>
    route.fulfill({ status: 503, body: 'Docker unavailable' })
  )
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'Docker unavailable' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'atlas', exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'atlas', exact: true }).click()
  await signIn(page)
  let count = 0
  await page.route('**/api/action', (route) => {
    count++
    return route.fulfill({ status: 401, body: 'Sign in first.' })
  })
  await page.getByRole('button', { name: 'Fetch origin', exact: true }).first().click()
  await expect(page.getByRole('dialog')).toContainText('Your session expired')
  expect(count).toBe(1)
  await page.getByLabel('Dashboard password').fill('demo-password')
  await page.getByRole('dialog').getByRole('button', { name: 'Sign in', exact: true }).click()
  expect(count).toBe(1)
})
test('desktop/mobile themes and navigation render without horizontal page overflow', async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem('devkit.swift-theme', 'rn-light'))
  await page.goto('/#/projects')
  await expect(page.getByRole('link', { name: 'atlas', exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/devkit-swift-desktop-light.png', fullPage: true })
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Theme', exact: true }).hover()
  await page.getByRole('menuitemradio', { name: 'RN', exact: true }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'rn')
  await page.screenshot({ path: '/tmp/devkit-swift-desktop-dark.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: '/tmp/devkit-swift-mobile.png', fullPage: true })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('button', { name: /navigation/i }).click()
  await page.getByRole('menuitem', { name: /^Containers \(/ }).click()
  await expect(page.getByRole('heading', { name: 'Containers', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('create project preserves instructions, rejects duplicates, cancels and refreshes the catalog', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/#/projects')
  await page.getByRole('button', { name: 'Create Project', exact: true }).click()
  await expect(page.getByLabel('Dashboard password')).toBeVisible()
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await signIn(page)
  let created = false
  const actions: Record<string, unknown>[] = []
  await page.route('**/api/action', async (route) => {
    const request = route.request().postDataJSON()
    actions.push(request)
    if (request.name === 'atlas')
      return route.fulfill({
        status: 400,
        body: 'A file or folder named atlas already exists. Nothing was changed.',
      })
    created = true
    return route.fulfill({ json: { id: 'fixture-job', title: 'Create project new-app' } })
  })
  await page.route('**/api/projects', async (route) => {
    const response = await route.fetch()
    const data = await response.json()
    if (created)
      data.projects.push({
        ...data.projects[0],
        name: 'new-app',
        path: '/work/new-app',
        repo: '/work/new-app/.git',
        checkouts: [],
      })
    return route.fulfill({ json: data })
  })
  const open = () => page.getByRole('button', { name: 'Create Project', exact: true }).click()
  await open()
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  expect(actions).toHaveLength(0)
  await open()
  await page.getByLabel('Project name', { exact: true }).fill('atlas')
  await page.getByLabel('Git remote URL').fill('git@example.test:team/new-app.git')
  const notes = 'Use Go. <script>literal</script> $(do-not-run)'
  await page.getByLabel('Additional agent instructions').fill(notes)
  await page.getByText('View preserved devkit guidance', { exact: true }).click()
  await expect(page.locator('.template-guidance')).toContainText('devkit')
  await page.screenshot({ path: '/tmp/devkit-create-project-desktop.png', fullPage: true })
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Create Project', exact: true })
    .click()
  await expect(page.getByRole('dialog')).toContainText('already exists')
  await expect(page.getByLabel('Additional agent instructions')).toHaveValue(notes)
  await page.getByLabel('Project name', { exact: true }).fill('new-app')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByText('View preserved devkit guidance', { exact: true }).click()
  await page.screenshot({ path: '/tmp/devkit-create-project-mobile.png', fullPage: true })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Create Project', exact: true })
    .click()
  await expect(page.getByRole('dialog')).toContainText('Completed synthetic action')
  expect(actions[1]).toEqual({
    action: 'project-create',
    name: 'new-app',
    remote: 'git@example.test:team/new-app.git',
    agent_notes: notes,
  })
  await page.keyboard.press('Escape')
  await expect(page.getByRole('link', { name: 'new-app', exact: true })).toBeVisible()
  expect(errors).toEqual([])
})
