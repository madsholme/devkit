import { describe, expect, it } from 'vitest'
import { projectInventory } from '../src/project-inventory'
import type { Checkout, Container, Overview, Project, VM } from '../src/types'
const tree = (path: string, extra = {}): Checkout => ({
  path,
  project: 'atlas',
  branch: path,
  containers: [],
  worktree_key: path,
  git: true,
  docker: true,
  note: '',
  previews: [],
  git_controls: { actions: [], note: '' },
  head: 'abcdef',
  changes: 0,
  upstream: 'origin/main',
  ahead: 0,
  behind: 0,
  subject: 'Commit',
  committed: 0,
  ...extra,
})
const project = (main: Checkout, worktrees: Checkout[] = []): Project => ({
  name: 'atlas',
  path: main.path,
  main,
  worktrees,
})
const container = (extra = {}): Container =>
  ({
    id: 'a'.repeat(64),
    name: 'database',
    registered: true,
    project: 'atlas',
    ...extra,
  } as Container)
const vm = (extra = {}): VM => ({ id: 'qemu:1', name: 'guest', project: 'atlas', ...extra } as VM)
describe('project inventory ownership', () => {
  it('deduplicates registered containers and uses refreshed capabilities', () => {
    const old = container({ actions: ['stop'] }),
      fresh = container({ actions: ['start'] })
    const [result] = projectInventory(
      [project(tree('/main', { containers: [old] }))],
      [fresh],
      null,
      []
    )
    expect(result!.containers).toHaveLength(1)
    expect(result!.containers[0]!.checkout!.path).toBe('/main')
    expect(result!.containers[0]!.container.actions).toEqual(['start'])
  })
  it('joins managed services by unique ID and keeps unrelated compose containers out', () => {
    const c = container({ registered: false, managed: true, preview: 'review', service: 'web' })
    const overview = {
      previews: [
        { name: 'review', worktree: '/feature', services: [{ name: 'web', id: 'a'.repeat(12) }] },
      ],
    } as Overview
    const [result] = projectInventory(
      [project(tree('/main'), [tree('/feature')])],
      [c, container({ id: 'b'.repeat(64), registered: false })],
      overview,
      []
    )
    expect(result!.containers).toHaveLength(1)
    expect(result!.containers[0]!.checkout!.path).toBe('/feature')
    const [collision] = projectInventory(
      [project(tree('/main'), [tree('/feature')])],
      [c, container({ id: 'a'.repeat(12) + 'b'.repeat(52), registered: false })],
      overview,
      []
    )
    expect(collision!.containers[0]!.checkout).toBeUndefined()
  })
  it('uses server worktree keys and preserves ambiguous claims at project level', () => {
    const main = tree('/main'),
      feature = tree('/feature')
    const [unique] = projectInventory([project(main, [feature])], [], null, [
      vm({ worktree_key: '/feature' }),
    ])
    expect(unique!.vms[0]!.checkout!.path).toBe('/feature')
    const [ambiguous] = projectInventory([project(main, [feature])], [], null, [
      vm({ worktree_note: 'Mapped by multiple worktrees' }),
    ])
    expect(ambiguous!.vms[0]!.checkout).toBeUndefined()
    expect(ambiguous!.vms[0]!.note).toBe('Mapped by multiple worktrees')
  })
  it('does not duplicate project-only VMs across clones with the same project identifier', () => {
    const results = projectInventory(
      [project(tree('/clone-a')), project(tree('/clone-b'))],
      [],
      null,
      [vm()]
    )
    expect(results.flatMap((r) => r.vms)).toEqual([])
  })
  it('keeps duplicate VM identities and conflicting registration paths unassigned', () => {
    const projects = [
      project(tree('/main', { containers: [container()], worktree_key: '/main' }), [
        tree('/feature'),
      ]),
    ]
    const [result] = projectInventory(projects, [container({ worktree: '/feature' })], null, [
      vm(),
      vm({ id: 'qemu:2' }),
    ])
    expect(result!.vms.every((v) => !v.checkout)).toBe(true)
    expect(result!.containers[0]!.checkout).toBeUndefined()
  })
  it('keeps project structure available without runtime providers or VM worktree keys', () => {
    const [result] = projectInventory([project(tree('/main'))], [], null, [vm()])
    expect(result!.checkouts).toHaveLength(1)
    expect(result!.vms[0]!.checkout).toBeUndefined()
    expect(result!.containers).toEqual([])
  })
})
