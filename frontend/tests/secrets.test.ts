import { describe, expect, it } from 'vitest'
import { transientSecret, validRetention } from '../src/secrets'
describe('secret value lifetime and cleanup settings', () => {
  it('does not restore a revealed value after the context has changed', async () => {
    let finish!: (value: string) => void
    const state = transientSecret()
    const pending = state.reveal(() => new Promise<string>((resolve) => { finish = resolve }))
    state.clear()
    finish('sensitive-value')
    await pending
    expect(state.value.value).toBe('')
    await state.reveal(async () => 'current-value')
    expect(state.value.value).toBe('current-value')
    state.clear()
    expect(state.value.value).toBe('')
  })
  it('accepts configurable retention and never without silently accepting invalid durations', () => {
    for (const value of ['7d', '30d', '48h', 'never']) expect(validRetention(value)).toBe(true)
    for (const value of ['0d', '-1d', '', 'later']) expect(validRetention(value)).toBe(false)
  })
})
