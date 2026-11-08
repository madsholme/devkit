import { describe, expect, it } from 'vitest'
import { documentLink, renderMarkdown } from '../src/markdown'

describe('project Markdown', () => {
  it('renders plans with headings, tables, code and unique outline anchors', () => {
    const result = renderMarkdown(
      '# Plan\n\n## Steps\n\n- First\n- Second\n\n| Task | State |\n| --- | --- |\n| Read | Done |\n\n```sh\necho "<safe>"\n```\n\n## Steps',
      'docs/plan.md'
    )
    expect(result.html).toContain('<table>')
    expect(result.html).toContain('&lt;safe&gt;')
    expect(result.headings.map((h) => h.id)).toEqual(['plan', 'steps', 'steps-1'])
  })
  it('never renders raw HTML, executable links or images', () => {
    const result = renderMarkdown(
      '<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n[x](javascript:alert(1))\n\n![alt](https://tracking.invalid/a.png)\n\n[local](file:///etc/passwd)\n\n[safe](https://example.test)',
      'docs/plan.md'
    ).html
    expect(result).not.toContain('<script>')
    expect(result).not.toContain('<img')
    expect(result).not.toContain('href="javascript:')
    expect(result).not.toContain('href="file:')
    expect(result).toContain('rel="noopener noreferrer"')
  })
  it('resolves only supported document links and safe external URLs', () => {
    expect(documentLink('../specs/design.md#scope', 'docs/plans/plan.md')).toEqual({
      path: 'docs/specs/design.md',
      anchor: 'scope',
    })
    expect(documentLink('../../AGENTS.md', 'docs/plans/plan.md')?.path).toBe('AGENTS.md')
    for (const link of [
      '../../../secret.md',
      '//tracking.invalid',
      'file:///etc/passwd',
      'data:text/html,x',
      '../.secret.md',
      'https://user:token@host/a',
      '..%2f..%2f.env',
    ])
      expect(documentLink(link, 'docs/plan.md')).toBeNull()
  })
})

it('navigates between root README and docs', () => {
 expect(documentLink('../../readme.md', 'docs/plans/plan.md')?.path).toBe('readme.md')
 expect(documentLink('docs/plan.md', 'README.md')?.path).toBe('docs/plan.md')
})
