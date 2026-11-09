export interface GitState {
  head: string
  changes: number
  upstream: string
  ahead: number
  behind: number
  subject: string
  committed: number | null
}
export interface ProjectPreview {
  name: string
  mode: string
  status: string
  url: string
}
export interface Container {
  worktree?: string
  id: string
  name: string
  image: string
  status: string
  project: string
  preview: string
  service: string
  managed: boolean
  registered: boolean
  ports: string[]
  actions?: string[]
  control_note?: string
}
export interface Checkout extends GitState {
  worktree_key?: string
  path: string
  branch: string
  project: string
  git: boolean
  docker: boolean
  note: string
  previews: ProjectPreview[]
  containers?: Container[]
  git_controls: { actions: string[]; note: string }
}
export interface Project {
  name: string
  path: string
  main: Checkout
  worktrees: Checkout[]
}
export interface VM {
  worktree_key?: string
  worktree_note?: string
  uuid?: string
  id: string
  name: string
  project: string
  project_note?: string
  state: string
  source: string
  pid?: number
  display: string
  memory_bytes?: number
  vcpus?: number
  disk_bytes: number | null
  graphics: string[]
  cpu_percent: number | null
  cpu_status: string
  rss_bytes?: number
  control_id?: string
  actions?: string[]
  control_note?: string
  connection_note?: string
  connections: { kind: string; tunnel?: string; command?: string }[]
}
export interface Port {
  port: number
  protocol: string
  routed: boolean
}
export interface Service {
  name: string
  status: string
  web: boolean
  id: string
  image: string
  ports: Port[]
  urls: string[]
  started: string | null
}
export interface Preview extends ProjectPreview {
  project: string
  branch: string
  commit: string
  worktree: string
  repository: string
  services: Service[]
  started: string | null
  head: string | null
  changes: number | null
  behind: number | null
  source_branch: string | null
  share: { url: string; host: string; expires: string; fixed: boolean } | null
}
export interface Worktree extends GitState {
  path: string
  branch: string
  previews: string[]
}
export interface Repository {
  path: string
  worktrees: Worktree[]
  orphans: { name: string; mode: string; worktree: string }[]
}
export interface Host {
  name: string
  domain: string
  version: string
  commit: string
  certificate_days: number | null
  certificate_expires: string
  running_containers: number
  services: { name: string; status: string; detail: string; url?: string }[]
  disk: { type: string; size: string; reclaimable: string }[]
}
export interface Overview {
  previews: Preview[]
  repositories: Repository[]
  host: Host
}
export interface Session {
  signed_in: boolean
  password_set: boolean
  pair?: boolean
  share?: {
    prefix: string
    domain: string
    hosts: string[]
    max_hours: number
    default_hours: number
  } | null
}
export interface HistoryEvent {
  time: string
  action: string
  ok: boolean
  preview: string
  worktree: string
  detail: string
  by: string
}
export interface Sample {
  t: number
  cpu: number
  load: number[]
  mem_used: number
  mem_total: number
  swap_used: number
  swap_total: number
  disk_used: number
  disk_total: number
}
export interface Resources {
  history: Sample[]
  cores: number
  uptime: number
  interval: number
  containers: {
    name: string
    preview: string
    service: string
    cpu: number
    mem: string
    net: string
    block: string
    pids: string
  }[]
  processes: {
    pid: number
    name: string
    command: string
    rss: number
    user: string
    cpu: number
  }[]
}
export interface ContainerDetails {
  name: string
  id: string
  status: string
  exit_code: number
  image: string
  image_id: string
  created: string
  started: string
  restart_policy: string
  restart_count: number
  user: string
  working_dir: string
  entrypoint: string[]
  command: string[]
  health: {
    status: string
    test: string[]
    failing_streak: number
    last_exit: number
    last_output: string
  }
  ports: Port[]
  networks: { name: string; ip: string; aliases: string[] }[]
  mounts: { destination: string; type: string; source: string; rw: boolean }[]
  env: { name: string; value: string }[]
  labels: Record<string, string>
}
export interface ActionRequest {
  name?: string
  remote?: string
  agent_notes?: string
  github_owner?: string
  github_repo?: string
  github_public?: boolean
  github_push?: boolean
  action: string
  preview?: string
  worktree?: string
  repo?: string
  vm?: string
  container?: string
  confirm?: string
  host?: string
  hours?: number
}
export interface LogTarget {
  preview?: string
  service?: string
  container?: string
}
export interface Pairing {
  qr: string[]
  url: string
  code: string
  expires: string
}
