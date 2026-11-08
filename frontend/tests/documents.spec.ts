import { readFileSync } from 'node:fs'
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
  await page.getByRole('combobox', { name: 'Document selector view' }).click()
  await page.getByRole('option', { name: 'Files only', exact: true }).click()
  await expect(page.getByLabel('Markdown source')).toHaveValue(/Keep my draft/)

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

test('document selector switches between tree, filenames and paths while preserving search and selection', async ({
  page,
}) => {
  await page.route('**/api/documents?*', async (route) => {
    const response = await route.fetch()
    const data = await response.json()
    data.files.push({ path: 'docs/archive/design.md', editable: true })
    return route.fulfill({ json: data })
  })
  await page.goto(url)
  const chooseView = async (name: string) => {
    await page.getByRole('combobox', { name: 'Document selector view' }).click()
    await page.getByRole('option', { name, exact: true }).click()
  }
  await page.getByRole('button', { name: 'Collapse all', exact: true }).click()
  await chooseView('Files only')
  const list = page.getByRole('navigation', { name: 'Markdown files' })
  await expect(page.getByRole('tree', { name: 'Markdown files' })).toBeHidden()
  const file = list.getByRole('button', { name: 'docs/specs/design.md', exact: true })
  await expect(file).toHaveText('design.md')
  await expect(list.getByText('design.md', { exact: true })).toHaveCount(2)
  await expect(file).toHaveAttribute('title', 'docs/specs/design.md')
  await page.getByLabel('Filter documents').fill('specs/')
  await expect(list.getByRole('button')).toHaveCount(1)
  await chooseView('Flat paths')
  await expect(file).toHaveText('docs/specs/design.md')
  await file.click()
  await expect(
    page.getByRole('heading', { name: 'Design specification', exact: true })
  ).toBeVisible()
  await expect(file).toHaveAttribute('aria-current', 'page')
  await page.getByLabel('Filter documents').fill('')
  await page.reload()
  await expect(page.getByRole('combobox', { name: 'Document selector view' })).toContainText(
    'Flat paths'
  )
  await expect(file).toHaveAttribute('aria-current', 'page')
  await chooseView('Folder tree')
  await expect(page.getByRole('treeitem', { name: 'design.md', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Collapse all', exact: true }).click()
  await chooseView('Files only')
  await page.screenshot({ path: '/tmp/devkit-documents-files-only.png', fullPage: true })
  await chooseView('Folder tree')
  await expect(page.getByRole('treeitem', { name: 'docs', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false'
  )
  await chooseView('Flat paths')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('document categories combine with search in tree and flat views', async ({ page }) => {
  const paths = ['README.md', 'docs/plans/m1-setup.md', 'docs/milestone-2.md', 'docs/plans/release.md', 'docs/specs/design.md']
  await page.route('**/api/documents?*', route => route.fulfill({ json: {
    files: paths.map(path => ({ path, editable: path.startsWith('docs/') })), warnings: [],
  } }))
  await page.goto(url)
  await page.getByRole('combobox', { name: 'Document category' }).click()
  await page.getByRole('option', { name: 'Milestones', exact: true }).click()
  await expect(page.getByRole('treeitem', { name: 'm1-setup.md', exact: true })).toBeVisible()
  await expect(page.getByRole('treeitem', { name: 'milestone-2.md', exact: true })).toBeVisible()
  await expect(page.getByRole('treeitem', { name: 'release.md', exact: true })).toHaveCount(0)
  await page.getByRole('combobox', { name: 'Document selector view' }).click()
  await page.getByRole('option', { name: 'Flat paths', exact: true }).click()
  await page.getByRole('textbox', { name: 'Filter documents' }).fill('setup')
  await expect(page.getByRole('button', { name: 'docs/plans/m1-setup.md', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'docs/milestone-2.md', exact: true })).toHaveCount(0)
  await page.getByRole('textbox', { name: 'Filter documents' }).fill('')
  for (const [category, path] of [['Plans', 'docs/plans/release.md'], ['Specs', 'docs/specs/design.md'], ['README', 'README.md']]) {
    await page.getByRole('combobox', { name: 'Document category' }).click()
    await page.getByRole('option', { name: category, exact: true }).click()
    await expect(page.getByRole('button', { name: path, exact: true })).toBeVisible()
  }
})

test('pictures, Markdown images and Mermaid flowcharts render safely on desktop and mobile', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  let externalRequests = 0
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="240"><rect width="640" height="240" fill="#157a9c"/><text x="40" y="130" fill="white" font-size="32">Project preview</text></svg>'
  const content = '# Media guide\n\n<picture>\n<source media="(prefers-color-scheme: dark)" srcset="docs/dark.svg">\n<img src="docs/light.svg" alt="Project screenshot" width="640" onerror="alert(1)">\n</picture>\n\n![Logo](logo.svg)\n\n```mermaid\nflowchart LR\n A[Read README] --> B{Choose checkout}\n B --> C[View pictures]\n B --> D[Read flowchart]\n```\n\n```mermaid\nflowchart LR\n A[broken\n```\n\n![External preview](https://images.example.test/preview.png)'
  await page.route('**/api/document?*', route => route.fulfill({ json: { path: 'README.md', content, editable: false, version: 'a'.repeat(64) } }))
  await page.route('**/api/document-image?*', route => route.fulfill({ contentType: 'image/svg+xml', body: svg }))
  await page.route('https://images.example.test/**', route => { externalRequests++; return route.fulfill({ contentType: 'image/svg+xml', body: svg }) })
  await page.goto(url + '?doc=README.md')
  const picture = page.getByRole('img', { name: 'Project screenshot', exact: true })
  await expect(picture).toBeVisible()
  await expect.poll(() => picture.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0)
  await expect(page.getByRole('img', { name: 'Mermaid flowchart', exact: true })).toBeVisible()
  await expect(page.getByText('Could not render this flowchart. Check its Mermaid syntax.')).toBeVisible()
  expect(externalRequests).toBe(0)
  await page.getByRole('button', { name: 'Load external image: External preview' }).click()
  await expect.poll(() => externalRequests).toBe(1)
  await expect(page.getByRole('img', { name: 'External preview', exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/devkit-media-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(picture).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: '/tmp/devkit-media-mobile.png', fullPage: true })
  expect(errors).toEqual([])
})

test('the repository README renders its actual pictures and flowchart', async ({ page }) => {
 const content = readFileSync(new URL('../../README.md', import.meta.url), 'utf8')
 await page.route('**/api/document?*', route => route.fulfill({ json: { path: 'README.md', content, editable: false, version: 'b'.repeat(64) } }))
 await page.route('**/api/document-image?*', route => {
   const path = new URL(route.request().url()).searchParams.get('path')!
   if (!/^docs\/images\/[a-z-]+\.png$/.test(path)) return route.abort()
   return route.fulfill({ contentType: 'image/png', body: readFileSync(new URL('../../'+path, import.meta.url)) })
 })
 await page.goto(url + '?doc=README.md')
 await expect(page.getByRole('img', { name: 'Mermaid flowchart' })).toBeVisible()
 await expect(page.locator('.document-diagram > .note')).toHaveCount(0)
 for (const img of await page.locator('.markdown-document picture img').all()) {
   await img.scrollIntoViewIfNeeded()
   await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0)
 }
 await expect(page.locator('.markdown-document picture')).toHaveCount(2)
})
