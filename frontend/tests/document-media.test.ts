// @vitest-environment jsdom
import { expect, it } from 'vitest'
import { renderMarkdown } from '../src/markdown'

it('renders local Markdown images through the checkout image endpoint', () => {
 const html = renderMarkdown('![Diagram](../images/one.png)', 'docs/plans/plan.md', '/repo').html
 expect(html).toContain('<img')
 expect(html).toContain('worktree=%2Frepo')
 expect(html).toContain('path=docs%2Fimages%2Fone.png')
 expect(html).not.toContain('src="../')
})
it('renders picture sources without admitting executable HTML attributes', () => {
 const html = renderMarkdown('<picture><source media="(prefers-color-scheme: dark)" srcset="docs/dark.png"><img src="docs/light.png" onerror="alert(1)" alt="Preview"></picture>', 'README.md', '/repo').html
 expect(html).toContain('<picture>')
 expect(html).toContain('<source')
 expect(html).toContain('path=docs%2Fdark.png')
 expect(html).not.toContain('onerror')
 expect(html).not.toContain('src="docs/')
})
it('leaves remote images opt-in and rejects unsafe local paths and scripts', () => {
 const html = renderMarkdown('![remote](https://tracker.invalid/image.png)\n\n![bad](../../secret.png)\n\n<script>alert(1)</script>', 'README.md', '/repo').html
 expect(html).not.toContain('src="https://')
 expect(html).toContain('Load external image')
 expect(html).not.toContain('<script>')
 expect(html).not.toContain('path=..')
})
it('marks Mermaid fences for rendering but preserves ordinary code and escaped source', () => {
 const html = renderMarkdown('```mermaid\nflowchart LR\n A-->B\n```\n\n```html\n<img src=x>\n```','README.md','/repo').html
 expect(html).toContain('data-mermaid')
 expect(html).toContain('flowchart LR')
 expect(html).not.toContain('<img src=x>')
})

import { imageTarget } from '../src/document-media'
import { flowchartError } from '../src/document-diagrams'
it('rejects hidden, encoded traversal, remote credentials and active image protocols', () => {
 for (const src of ['../secret.png', '%2e%2e/secret.png', '.private/p.png', 'file:///a.png', '//host/a.png', 'data:image/png;base64,AA', 'https://u:p@host/a.png'])
  expect(imageTarget(src, 'README.md', '/repo')).toBeNull()
})
it('rejects diagram configuration and resources, while accepting the README flowchart', () => {
 expect(flowchartError('flowchart LR\n A["User"] --> B["Host"]')).toBe('')
 for (const source of ['sequenceDiagram', 'flowchart LR\n%%{init: {securityLevel: "loose"}}%%', 'flowchart LR\nA@{img: "x"}', 'graph TD\nA["https://host/image.png"]', 'graph TD\n'+'a'.repeat(20001)])
  expect(flowchartError(source)).not.toBe('')
})
