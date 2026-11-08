import type { Checkout, Container, Overview, Project, VM } from './types'

export interface ScopedContainer {
  container: Container
  checkout?: Checkout
  note?: string
}
export interface ScopedVM {
  vm: VM
  checkout?: Checkout
  note?: string
}
export interface ProjectInventory {
  project: Project
  checkouts: Checkout[]
  containers: ScopedContainer[]
  vms: ScopedVM[]
}
export const projectLocation = (
  path: string,
  section = 'overview',
  tree?: string,
  resource?: string
) => ({
  name: 'project',
  params: { project: path, section },
  query: tree ? { tree, ...(resource ? { resource } : {}) } : {},
})

// This joins read-only inventory. Capabilities and action targets always come from the server.
export function projectInventory(
  projects: Project[],
  containers: Container[],
  overview: Overview | null,
  vms: VM[]
): ProjectInventory[] {
  const result = projects.map((project) => ({
    project,
    checkouts: [project.main, ...project.worktrees],
    containers: [] as ScopedContainer[],
    vms: [] as ScopedVM[],
  }))
  const trees = result.flatMap((inventory) =>
    inventory.checkouts.map((checkout) => ({ inventory, checkout }))
  )
  const allContainers = new Map<string, Container>()
  for (const { checkout } of trees)
    for (const c of checkout.containers || []) allContainers.set(c.id, c)
  // Fresh global inventory carries the latest status and capabilities.
  for (const c of containers) allContainers.set(c.id, c)
  for (const c of allContainers.values()) {
    const paths = new Set<string>()
    if (c.registered && c.worktree) paths.add(c.worktree)
    for (const { checkout } of trees) {
      if (c.registered && checkout.containers?.some((item) => item.id === c.id))
        paths.add(checkout.path)
    }
    if (c.managed) {
      for (const p of overview?.previews || []) {
        if (p.name !== c.preview) continue
        const service = p.services.find(
          (s) => s.name === c.service && s.id.length >= 12 && c.id.startsWith(s.id)
        )
        if (
          service &&
          [...allContainers.keys()].filter((id) => id.startsWith(service.id)).length === 1
        )
          paths.add(p.worktree)
      }
    }
    const matches = trees.filter((t) => paths.has(t.checkout.path))
    if (paths.size === 1 && matches.length === 1)
      matches[0]!.inventory.containers.push({ container: c, checkout: matches[0]!.checkout })
    else if (c.managed || c.registered) {
      // A known project can still contain an orphaned or ambiguous checkout.
      const owners = result.filter((i) =>
        i.checkouts.some((t) => t.project && t.project === c.project)
      )
      if (owners.length === 1)
        owners[0]!.containers.push({
          container: c,
          note:
            paths.size > 1 ? 'Conflicting worktree mappings' : 'Worktree not in project inventory',
        })
    }
  }
  for (const vm of vms) {
    if (!vm.project || vm.project_note) continue
    const claims = vm.worktree_key
      ? trees.filter(
          (t) => t.checkout.worktree_key === vm.worktree_key && t.checkout.project === vm.project
        )
      : []
    const exact = claims.length === 1 ? claims[0] : undefined
    if (exact) exact.inventory.vms.push({ vm, checkout: exact.checkout })
    else {
      const owners = result.filter((i) =>
        i.checkouts.some((t) => t.project && t.project === vm.project)
      )
      if (owners.length === 1)
        owners[0]!.vms.push({
          vm,
          note:
            vm.worktree_note ||
            (claims.length > 1 ? 'Mapped by multiple worktrees' : 'No unique worktree mapping'),
        })
    }
  }
  for (const inventory of result) {
    inventory.containers.sort((a, b) => a.container.name.localeCompare(b.container.name))
    inventory.vms.sort((a, b) => a.vm.name.localeCompare(b.vm.name))
  }
  return result
}
