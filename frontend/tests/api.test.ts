import { describe, expect, it } from 'vitest'
import { confirmation, targetQuery } from '../src/api'
import { safeURL } from '../src/format'
describe('action and inspection boundaries', () => {
  it('confirms exact immutable targets and preserves cleanup semantics', () => {
    expect(confirmation({ action: 'container-stop', container: 'a'.repeat(64) })?.confirm).toBe(
      'a'.repeat(64)
    )
    expect(confirmation({ action: 'vm-stop', vm: 'qmp:uuid' })?.confirm).toBe('qmp:uuid')
    expect(confirmation({ action: 'destroy', preview: 'demo-live' })?.confirm).toBe('demo-live')
    expect(confirmation({ action: 'gc', repo: '/work/demo/.git' })?.confirm).toBe('remove')
    expect(confirmation({ action: 'git-fetch', worktree: '/work/demo' })).toBeNull()
  })
  it('uses registered full IDs instead of preview aliases for details and logs', () => {
    expect(targetQuery({ container: 'a'.repeat(64), preview: 'wrong', service: 'wrong' })).toBe(
      `container=${'a'.repeat(64)}`
    )
    expect(targetQuery({ preview: 'demo & other', service: 'web' })).toBe(
      'preview=demo+%26+other&service=web'
    )
  })
  it('rejects executable URLs without damaging normal app links', () => {
    expect(safeURL('javascript:alert(1)')).toBeUndefined()
    expect(safeURL('https://demo.example.test/api')).toBe('https://demo.example.test/api')
  })
})
