import { test, expect, type Page } from '@playwright/test'

async function expectFits(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  for (const table of await page.locator('[data-inventory] [data-slot="table-container"]').all()) {
    expect(await table.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
  }
}

test('mobile previews keep actions and expanded services within the screen', async ({ page, context }) => {
  await context.addCookies([{ name: 'fixture', value: 'signed', url: 'http://127.0.0.1:4178' }])
  const actions: unknown[] = []
  page.on('request', request => { if (request.url().endsWith('/api/action')) actions.push(request.postDataJSON()) })
  await page.route('**/api/overview', async route => {
    const response = await route.fetch()
    const data = await response.json()
    data.previews[0].branch = 'feature/a-very-long-branch-name-for-mobile-layout-validation'
    data.previews[0].worktree = '/home/demo/Work/a-very-long-project-directory/worktrees/feature-search'
    data.previews[0].services[0].image = 'registry.example.test/team/a-very-long-container-image-name:development'
    await route.fulfill({ json: data })
  })
  for (const width of [390, 320, 767]) {
    await page.setViewportSize({ width, height: 844 })
    await page.goto('/#/previews')
    await expect(page.getByRole('button', { name: 'Actions for atlas-main' })).toBeVisible()
    await expectFits(page)
    const button = page.getByRole('button', { name: 'Actions for atlas-main' })
    const box = await button.boundingBox()
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(width)
    expect(box!.height).toBeGreaterThanOrEqual(44)
    await button.click()
    await page.getByRole('menuitem', { name: 'Destroy…', exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText('atlas-main')
    await page.getByRole('button', { name: 'Cancel', exact: true }).click()
    expect(actions).toHaveLength(0)
    const expand = page.getByRole('button', { name: 'Show containers for atlas-main' })
    if (await expand.getAttribute('aria-expanded') !== 'true') await expand.click()
    await expect(page.getByRole('button', { name: 'Details', exact: true })).toBeVisible()
    await expectFits(page)
    await page.getByRole('button', { name: 'Details', exact: true }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    if (width === 390) {
      await page.evaluate(() => { (document.activeElement as HTMLElement)?.blur(); window.scrollTo({ top: 0, behavior: 'instant' }) })
      await page.screenshot({ path: '/tmp/devkit-previews-mobile.png', fullPage: true, animations: 'disabled' })
    }
  }
  await page.getByLabel('Filter previews').fill('no-such-preview')
  await expect(page.getByText('No previews found')).toBeVisible()
})

test('desktop keeps its table and anonymous mobile visitors have no mutation controls', async ({ page }) => {
  await page.goto('/#/previews')
  await expect(page.getByRole('columnheader', { name: 'Actions', exact: true })).toBeVisible()
  expect(await page.locator('[data-inventory] table').first().evaluate(el => getComputedStyle(el).display)).toBe('table')
  await page.evaluate(() => { (document.activeElement as HTMLElement)?.blur(); window.scrollTo({ top: 0, behavior: 'instant' }) })
  await page.screenshot({ path: '/tmp/devkit-previews-desktop.png', fullPage: true, animations: 'disabled' })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectFits(page)
  await expect(page.getByRole('button', { name: /^Actions for / })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Open atlas-main', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Logs', exact: true }).first().click()
  await expect(page.getByRole('dialog')).toBeVisible()
})
