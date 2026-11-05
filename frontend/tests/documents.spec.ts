import { test, expect } from '@playwright/test'
const main = '/home/demo/Work/atlas'
const url = '/#/projects/' + encodeURIComponent(main) + '/documents'

test('project docs read Markdown, follow links, show agent guidance and switch checkouts', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto(url)
  await expect(page.getByRole('heading', { name: 'Project redesign', exact: true })).toBeVisible()
  await expect(
    page.getByRole('article', { name: 'Document content' }).getByRole('table')
  ).toBeVisible()
  await page.getByRole('link', { name: 'design spec', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Design specification', exact: true })
  ).toBeVisible()
  await page
    .getByRole('tree', { name: 'Markdown files' })
    .getByRole('treeitem', { name: 'AGENTS.md' })
    .click()
  await expect(page.getByRole('heading', { name: 'Agent guidance', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0)
  await page.getByRole('combobox', { name: 'Document checkout' }).click()
  const response = page.waitForRequest(
    (r) =>
      r.url().includes('/api/documents?') && new URL(r.url()).searchParams.get('worktree') !== main
  )
  await page.getByRole('option', { name: /feature\/search/ }).click()
  await response
  await expect(page).toHaveURL(/tree=/)
  await expect(page.getByRole('heading', { name: 'Project redesign', exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/devkit-documents-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: '/tmp/devkit-documents-mobile.png', fullPage: true })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  expect(errors).toEqual([])
})

test('document editing preserves drafts on conflicts and sign-in expiry, guards navigation and creates docs', async ({
  page,
}) => {
  const docs: Record<string, { content: string; version: string; editable: boolean }> = {
    'docs/plan.md': { content: '# Original plan\n', version: 'a'.repeat(64), editable: true },
    'AGENTS.md': { content: '# Guidance', version: 'b'.repeat(64), editable: false },
  }
  await page.route('**/api/documents?*', (route) =>
    route.fulfill({
      json: {
        files: Object.entries(docs).map(([path, doc]) => ({ path, editable: doc.editable })),
        warnings: [],
      },
    })
  )
  await page.route('**/api/document?*', (route) => {
    const path = new URL(route.request().url()).searchParams.get('path')!
    return route.fulfill({ json: { path, ...docs[path] } })
  })
  let outcome = 'conflict'
  let submitted: Record<string, unknown> = {}
  await page.route('**/api/action', (route) => {
    submitted = route.request().postDataJSON()
    if (outcome === 'conflict')
      return route.fulfill({ status: 400, body: 'Document changed on disk. Reload before saving.' })
    if (outcome === 'expired') return route.fulfill({ status: 401, body: 'Sign in first.' })
    docs[String(submitted.path)] = {
      content: String(submitted.content),
      version: 'c'.repeat(64),
      editable: true,
    }
    return route.fulfill({ json: { id: 'fixture-job', title: 'Save document' } })
  })
  await page.goto(url)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.getByLabel('Dashboard password').fill('demo-password')
  await page.getByRole('dialog').getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.getByLabel('Markdown source').fill('# Revised plan\n\nKeep my draft.')
  await page.getByRole('button', { name: 'Preview Markdown' }).click()
  await expect(page.getByRole('heading', { name: 'Revised plan', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Save document', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('changed on disk')
  await page.getByRole('button', { name: 'Edit source' }).click()
  await expect(page.getByLabel('Markdown source')).toHaveValue(/Keep my draft/)
  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('link', { name: 'Back to Projects', exact: true }).click()
  await expect(page.getByLabel('Markdown source')).toHaveValue(/Keep my draft/)
  outcome = 'expired'
  await page.getByRole('button', { name: 'Save document', exact: true }).click()
  await expect(page.getByLabel('Dashboard password')).toBeVisible()
  await page.getByLabel('Dashboard password').fill('demo-password')
  await page.getByRole('dialog').getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByLabel('Markdown source')).toHaveValue(/Keep my draft/)
  outcome = 'success'
  await page.getByRole('button', { name: 'Save document', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('Changes remain uncommitted')
  expect(submitted).toMatchObject({
    action: 'document-save',
    worktree: main,
    path: 'docs/plan.md',
    expected: 'a'.repeat(64),
  })
  await page.getByRole('button', { name: 'New document', exact: true }).click()
  await page.getByLabel('File path inside docs/').fill('specs/new.md')
  await page.getByLabel('Markdown source').fill('# New specification\n\n- First requirement')
  await page.screenshot({ path: '/tmp/devkit-documents-editor.png', fullPage: true })
  await page.getByRole('button', { name: 'Save document', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'New specification', exact: true })).toBeVisible()
  expect(submitted).toMatchObject({ path: 'docs/specs/new.md', expected: 'new' })
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.getByLabel('Markdown source').fill('unsaved again')
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('link', { name: 'Back to Projects', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Projects', exact: true })).toBeVisible()
})

test('document tree expands folders, reveals search matches and restores collapsed navigation', async ({
  page,
}) => {
  await page.goto(url)
  const tree = page.getByRole('tree', { name: 'Markdown files' })
  await expect(tree.getByRole('treeitem', { name: 'redesign.md', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Collapse all', exact: true }).click()
  await expect(tree.getByRole('treeitem', { name: 'docs', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false'
  )
  await expect(tree.getByRole('treeitem', { name: 'redesign.md', exact: true })).toHaveCount(0)
  await expect(tree.getByRole('treeitem', { name: 'AGENTS.md', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Expand all', exact: true }).click()
  await expect(tree.getByRole('treeitem', { name: 'redesign.md', exact: true })).toBeVisible()
  await expect(tree.getByRole('treeitem', { name: 'design.md', exact: true })).toBeVisible()
  await tree.getByRole('treeitem', { name: 'plans', exact: true }).click()
  await expect(tree.getByRole('treeitem', { name: 'redesign.md', exact: true })).toHaveCount(0)
  await tree.getByRole('treeitem', { name: 'plans', exact: true }).press('ArrowRight')
  await expect(tree.getByRole('treeitem', { name: 'redesign.md', exact: true })).toBeVisible()
  await tree.getByRole('treeitem', { name: 'plans', exact: true }).press('ArrowRight')
  await expect(tree.getByRole('treeitem', { name: 'redesign.md', exact: true })).toBeFocused()
  await page.getByRole('button', { name: 'Collapse all', exact: true }).click()
  await page.getByLabel('Filter documents').fill('design.md')
  await expect(tree.getByRole('treeitem', { name: 'docs', exact: true })).toHaveAttribute(
    'aria-expanded',
    'true'
  )
  await expect(tree.getByRole('treeitem', { name: 'design.md', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Collapse all', exact: true })).toBeDisabled()
  await page.getByLabel('Filter documents').fill('')
  await expect(tree.getByRole('treeitem', { name: 'docs', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false'
  )
  await page.getByRole('button', { name: 'Expand all', exact: true }).click()
  await tree.getByRole('treeitem', { name: 'design.md', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Design specification', exact: true })
  ).toBeVisible()
  await expect(tree.getByRole('treeitem', { name: 'design.md', exact: true })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  await page.getByLabel('Filter documents').fill('nothing-matches')
  await expect(page.getByText('No matching documents.')).toBeVisible()
  await page.getByLabel('Filter documents').fill('')
  await page.screenshot({ path: '/tmp/devkit-document-tree.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
