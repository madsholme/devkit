import { test, expect } from '@playwright/test'

test('desktop previews wrap long health summaries and keep actions on screen', async ({ page, context }) => {
  await context.addCookies([{ name: 'fixture', value: 'signed', url: 'http://127.0.0.1:4178' }])
  await page.route('**/api/overview', async route => {
    const response = await route.fetch()
    const data = await response.json()
    const preview = data.previews[0]
    preview.name = 'atlas-codex-child-profile-controls'
    preview.branch = 'codex/a-long-feature-branch-for-desktop-layout'
    preview.worktree = '/home/demo/.t3/worktrees/Atlas/codex-child-profile-controls'
    preview.services.push(...['api-checks', 'api', 'web-checks', 'postgres'].map((name, index) => ({
      ...preview.services[0], name: `atlas-${name}`, web: false, id: `${index}`.repeat(12),
      image: 'registry.example.test/team/very-long-service-image-name:development',
    })))
    await route.fulfill({ json: data })
  })
  for (const width of [1674, 1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/#/previews')
    await expect(page.getByRole('columnheader', { name: 'Actions', exact: true })).toBeVisible()
    for (const table of await page.locator('[data-inventory] [data-slot="table-container"]').all()) {
      expect(await table.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
    }
    await expect(page.getByText('atlas-api-checks healthy', { exact: false })).toBeVisible()
    const action = page.getByRole('button', { name: 'Actions for atlas-codex-child-profile-controls' })
    const box = await action.boundingBox()
    expect(box!.x + box!.width).toBeLessThanOrEqual(width)
    await page.getByRole('button', { name: 'Show containers for atlas-codex-child-profile-controls' }).click()
    for (const table of await page.locator('[data-inventory] [data-slot="table-container"]').all()) {
      expect(await table.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
    }
    if (width === 1440) await page.screenshot({ path: '/tmp/devkit-preview-desktop-expanded.png', fullPage: true })
    await page.getByRole('button', { name: 'Show containers for atlas-codex-child-profile-controls' }).click()
    if (width === 1440) await page.screenshot({ path: '/tmp/devkit-preview-desktop-collapsed.png', fullPage: true })
  }
})

test('preview names and arrows share the information panel while external open stays separate', async ({ page }) => {
  await page.goto('/#/previews')
  const name = page.getByRole('button', { name: 'atlas-main', exact: true })
  const arrow = page.getByRole('button', { name: 'Show containers for atlas-main' })
  await name.click()
  await expect(name).toHaveAttribute('aria-expanded', 'true')
  await expect(arrow).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByRole('button', { name: 'Details', exact: true })).toBeVisible()
  await arrow.click()
  await expect(name).toHaveAttribute('aria-expanded', 'false')
  await name.focus()
  await page.keyboard.press('Enter')
  await expect(arrow).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByRole('link', { name: 'Open atlas-main', exact: true })).toHaveAttribute('href', 'https://atlas.example.test/')
  await expect(page).toHaveURL(/\/previews$/)
})

test('preview Secrets action opens the exact main or linked checkout without revealing values', async ({ page, context }) => {
  await context.addCookies([{ name: 'fixture', value: 'signed', url: 'http://127.0.0.1:4178' }])
  const calls: Record<string, unknown>[] = []
  await page.route('**/api/secrets', async route => {
    const q = route.request().postDataJSON()
    calls.push(q)
    await route.fulfill({ json: {
      context: { project: 'project1', branch: 'branch1', worktree: 'tree1', root: q.root },
      resources: [{ id: 'branch1', kind: 'branch', name: q.root.endsWith('atlas-review') ? 'feature/search' : 'main', state: 'active' }],
      counts: { project1: 0, tree1: 0 }, records: [],
    } })
  })
  for (const [preview, root] of [['atlas-search', '/home/demo/Work/atlas-review'], ['atlas-main', '/home/demo/Work/atlas']]) {
    await page.goto('/#/previews')
    await page.getByRole('button', { name: `Actions for ${preview}` }).click()
    await page.getByRole('menuitem', { name: 'Secrets', exact: true }).click()
    await expect(page.getByRole('region', { name: 'Project secrets' })).toBeVisible()
    await expect.poll(() => calls.some(q => q.operation === 'list' && q.root === root && q.scope === 'worktree')).toBe(true)
    const query = new URLSearchParams(page.url().split('?')[1])
    expect(query.get('tree')).toBe(root)
    expect(query.get('resource')).toBe('secrets')
  }
  expect(calls.some(q => q.operation === 'get' || q.reveal)).toBe(false)
})

test('preview Secrets action refuses missing or ambiguous checkout associations', async ({ page, context }) => {
  await context.addCookies([{ name: 'fixture', value: 'signed', url: 'http://127.0.0.1:4178' }])
  for (const ambiguous of [false, true]) {
    await page.route('**/api/projects', async route => {
      const response = await route.fetch()
      const data = await response.json()
      if (ambiguous) data.projects.push({ ...data.projects[0], path: '/duplicate-project' })
      else data.projects = []
      await route.fulfill({ json: data })
    })
    await page.goto('/#/previews')
    await page.getByRole('button', { name: 'Actions for atlas-main' }).click()
    await expect(page.getByRole('menuitem', { name: 'Secrets', exact: true })).toBeDisabled()
    await page.keyboard.press('Escape')
    await page.unroute('**/api/projects')
  }
})
