import { expect, it } from 'vitest'
import { matchesDocumentCategory } from '../src/document-filter'

it('recognizes categories from path tokens without matching unrelated substrings', () => {
 for (const path of ['docs/milestones/launch.md', 'docs/plans/2026-m1-setup.md', 'docs/M2.md', 'docs/milestone-3.md'])
  expect(matchesDocumentCategory(path, 'milestones')).toBe(true)
 for (const path of ['docs/platform1.md', 'docs/team1.md', 'docs/m1abc.md'])
  expect(matchesDocumentCategory(path, 'milestones')).toBe(false)
 expect(matchesDocumentCategory('docs/plans/delivery.md', 'plans')).toBe(true)
 expect(matchesDocumentCategory('docs/implementation-plan.md', 'plans')).toBe(true)
 expect(matchesDocumentCategory('docs/specifications/design.md', 'specs')).toBe(true)
 expect(matchesDocumentCategory('docs/design-spec.md', 'specs')).toBe(true)
 expect(matchesDocumentCategory('readme.MD', 'readme')).toBe(true)
 expect(matchesDocumentCategory('docs/README.md', 'readme')).toBe(true)
 expect(matchesDocumentCategory('docs/readme-notes.md', 'readme')).toBe(false)
 expect(matchesDocumentCategory('AGENTS.md', 'all')).toBe(true)
 expect(matchesDocumentCategory('AGENTS.md', 'plans')).toBe(false)
})
