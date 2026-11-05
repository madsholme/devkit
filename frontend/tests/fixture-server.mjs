// Isolated synthetic host for UI acceptance. No Docker, Git, VM or host API calls.
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
const root = fileURLToPath(new URL('../../web/dist/', import.meta.url))
const now = new Date().toISOString()
const id = 'a'.repeat(64)
const git = {
  head: '42bc6e8d29',
  changes: 0,
  upstream: 'origin/main',
  ahead: 0,
  behind: 0,
  subject: 'Add health checks for the API',
  committed: Math.floor(Date.now() / 1000) - 3600,
}
const preview = {
  name: 'atlas-main',
  mode: 'live',
  status: 'healthy',
  url: 'https://atlas.example.test',
  project: 'atlas',
  branch: 'main',
  commit: '42bc6e8d29',
  worktree: '/home/demo/Work/atlas',
  repository: '/home/demo/Work/atlas/.git',
  started: now,
  head: '42bc6e8d29',
  changes: 0,
  behind: 0,
  source_branch: 'main',
  share: null,
  services: [
    {
      name: 'web',
      status: 'healthy',
      web: true,
      id: 'e'.repeat(12),
      image: 'atlas:web',
      ports: [{ port: 3000, protocol: 'tcp', routed: true }],
      urls: ['https://atlas.example.test', 'https://atlas.example.test/api'],
      started: now,
    },
  ],
}
const registered = {
  id,
  name: 'atlas-db',
  image: 'postgres:17',
  status: 'running',
  project: 'atlas',
  preview: '',
  service: 'database',
  managed: false,
  registered: true,
  ports: ['127.0.0.1:5432 → 5432/tcp'],
  actions: ['stop'],
}
const featurePreview = {
  ...preview,
  name: 'atlas-search',
  branch: 'feature/search',
  worktree: '/home/demo/Work/atlas-review',
  url: 'https://atlas-search.example.test',
  services: [{ ...preview.services[0], id: 'd'.repeat(12), image: 'atlas:search' }],
}
const mainContainer = {
  ...registered,
  id: 'e'.repeat(64),
  name: 'atlas-web',
  image: 'atlas:web',
  registered: false,
  managed: true,
  preview: preview.name,
  service: 'web',
  ports: ['127.0.0.1:3000 → 3000/tcp'],
}
const featureContainer = {
  ...mainContainer,
  id: 'd'.repeat(64),
  name: 'atlas-search-web',
  image: 'atlas:search',
  preview: featurePreview.name,
}
const checkout = {
  ...git,
  worktree_key: 'checkout-main',
  path: '/home/demo/Work/atlas',
  branch: 'main',
  project: 'atlas',
  git: true,
  docker: true,
  note: '',
  previews: [preview],
  containers: [registered],
  git_controls: { actions: ['fetch', 'sync', 'push'], note: '' },
}
const vm = {
  id: 'vm-demo',
  worktree_key: 'checkout-main',
  name: 'atlas-lab',
  project: 'atlas',
  state: 'running',
  source: 'qemu',
  pid: 2048,
  display: 'spice',
  memory_bytes: 4294967296,
  vcpus: 4,
  disk_bytes: 42949672960,
  graphics: ['virtio-vga'],
  cpu_percent: 3.2,
  cpu_status: 'available',
  rss_bytes: 524288000,
  control_id: 'qmp:atlas-lab',
  actions: ['stop', 'restart'],
  connections: [
    {
      kind: 'spice',
      tunnel: 'ssh -N -L 127.0.0.1:5931:127.0.0.1:5931 demo@devhost',
      command: 'remote-viewer spice://127.0.0.1:5931',
    },
  ],
}
const sample = {
  t: Date.now() / 1000,
  cpu: 17,
  load: [0.7, 0.5, 0.4],
  mem_used: 12884901888,
  mem_total: 34359738368,
  swap_used: 0,
  swap_total: 8589934592,
  disk_used: 180388626432,
  disk_total: 549755813888,
}
const fixtures = {
  overview: {
    previews: [preview, featurePreview],
    repositories: [
      {
        path: checkout.path + '/.git',
        worktrees: [
          { ...git, path: checkout.path, branch: 'main', previews: [preview.name] },
          {
            ...git,
            path: '/home/demo/Work/atlas-review',
            worktree_key: 'checkout-search',
            branch: 'feature/search',
            changes: 3,
            previews: [],
          },
        ],
        orphans: [],
      },
    ],
    host: {
      name: 'devhost',
      domain: 'lab.example.test',
      version: '10d0784',
      commit: '10d0784',
      certificate_days: 64,
      certificate_expires: 'Jan 08 12:00:00 2027 GMT',
      services: [
        {
          name: 'T3 Code',
          status: 'up',
          url: 'https://t3.example.test',
          detail: 'Harness is running',
        },
        { name: 'Traefik', status: 'up', detail: 'Proxy is running' },
      ],
      running_containers: 8,
      disk: [{ type: 'Images', size: '12.5GB', reclaimable: '2.1GB (17%)' }],
    },
  },
  projects: {
    directory: '/home/demo/Work',
    warnings: [],
    projects: [
      {
        name: 'atlas',
        path: checkout.path,
        main: checkout,
        worktrees: [
          {
            ...checkout,
            path: '/home/demo/Work/atlas-review',
            worktree_key: 'checkout-search',
            branch: 'feature/search',
            changes: 3,
            previews: [featurePreview],
            containers: [],
          },
        ],
      },
      {
        name: 'beacon',
        path: '/home/demo/Work/beacon',
        main: {
          ...checkout,
          path: '/home/demo/Work/beacon',
          worktree_key: 'checkout-beacon',
          project: 'beacon',
          branch: 'main',
          subject: 'Update the worker schedule',
          previews: [],
          containers: [],
        },
        worktrees: [],
      },
    ],
  },
  containers: {
    containers: [
      registered,
      mainContainer,
      featureContainer,
      {
        ...registered,
        id: 'b'.repeat(64),
        name: 'cache',
        project: 'beacon',
        service: 'cache',
        registered: false,
        image: 'redis:7',
        ports: [],
        status: 'exited',
        actions: [],
      },
      {
        ...registered,
        id: 'c'.repeat(64),
        name: 'standalone-worker',
        project: '',
        registered: false,
        service: '',
        ports: [],
      },
    ],
    warnings: [],
  },
  vms: {
    vms: [
      vm,
      {
        ...vm,
        id: 'vm-search',
        name: 'atlas-search-lab',
        worktree_key: 'checkout-search',
        state: 'stopped',
        actions: ['start'],
        connections: [],
        control_id: 'vm-search',
      },
      {
        ...vm,
        id: 'vm-shared',
        name: 'atlas-shared',
        worktree_key: undefined,
        worktree_note: 'Mapped by multiple worktrees',
        actions: [],
        connections: [],
        control_id: undefined,
      },
    ],
    warnings: [],
  },
  history: {
    events: [
      {
        time: now,
        action: 'live',
        ok: true,
        preview: preview.name,
        detail: 'Created live preview',
        by: 'dashboard',
        worktree: checkout.path,
      },
      {
        time: new Date(Date.now() - 3600000).toISOString(),
        action: 'review',
        ok: false,
        preview: 'atlas-review',
        detail: 'Build check failed',
        by: 'cli',
        worktree: '/home/demo/Work/atlas-review',
      },
    ],
  },
  resources: {
    history: Array.from({ length: 60 }, (_, i) => ({
      ...sample,
      t: sample.t - (59 - i) * 10,
      cpu: 12 + Math.sin(i / 5) * 8,
    })),
    cores: 16,
    uptime: 450000,
    interval: 5,
    containers: [
      {
        name: 'atlas-db',
        preview: '',
        service: 'database',
        cpu: 1.5,
        mem: '256 MiB',
        net: '2.1 MB / 3.4 MB',
        block: '128 MB / 256 MB',
        pids: '12',
      },
    ],
    processes: [
      { pid: 124, name: 'devkit', command: 'devkit web', rss: 67108864, user: 'demo', cpu: 0.1 },
    ],
  },
  container: {
    name: 'atlas-db',
    id,
    status: 'running',
    exit_code: 0,
    image: 'postgres:17',
    image_id: 'sha256:abc123',
    created: now,
    started: now,
    restart_policy: 'unless-stopped',
    restart_count: 0,
    user: 'postgres',
    working_dir: '/app',
    entrypoint: ['docker-entrypoint.sh'],
    command: ['postgres'],
    health: {
      status: 'healthy',
      test: ['CMD', 'pg_isready'],
      failing_streak: 0,
      last_exit: 0,
      last_output: 'accepting connections',
    },
    ports: [],
    networks: [{ name: 'atlas', ip: '172.18.0.2', aliases: ['database'] }],
    mounts: [
      { destination: '/var/lib/postgresql/data', type: 'volume', source: 'atlas-db', rw: true },
    ],
    env: [
      { name: 'POSTGRES_PASSWORD', value: '••••••••' },
      { name: 'PGDATA', value: '/var/lib/postgresql/data' },
    ],
    labels: { 'com.docker.compose.project': 'atlas' },
  },
  pair: {
    qr: [
      '111111100110001010011001001111111',
      '100000100100100110101110101000001',
      '101110101010101010001110101011101',
      '101110101110000000100011101011101',
      '101110101110101110001101101011101',
      '100000101100101001110100001000001',
      '111111101010101010101010101111111',
      '000000001000000010011111100000000',
      '101111100110111011010010001111100',
      '001000011001010001011001001101101',
      '100100110110011101000010010010110',
      '111011011001010001101110000011110',
      '011111111011001110001010110011010',
      '000011010111010100100001001001011',
      '001110101011010110001110011110010',
      '110001000001101100100110111010100',
      '011100101111100111101011110111001',
      '001001010111101010111011011101101',
      '101100111111111110100010101110110',
      '001010010000001100011101000111111',
      '011111100111010000110011110111001',
      '100110011010011111001101001001001',
      '101111111010111001010100110001110',
      '101000000101000010011100000111110',
      '101000101110001001010010111111000',
      '000000001011000011010000100010101',
      '111111100111100111001111101010110',
      '100000101010010101101100100011111',
      '101110101100110110000010111111011',
      '101110101000101100100011000010111',
      '101110101011101110101110111101000',
      '100000100111011100100101010011100',
      '111111101101100101010011110101010',
    ],
    url: 'https://t3.example.test/pair?token=synthetic-only',
    code: '123-456',
    expires: new Date(Date.now() + 600000).toISOString(),
  },
}
const session = (signed) => ({
  signed_in: signed,
  password_set: true,
  pair: true,
  share: {
    prefix: 'share-',
    domain: 'example.test',
    hosts: ['demo'],
    max_hours: 24,
    default_hours: 4,
  },
})
createServer(async (req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1')
  const endpoint = url.pathname.slice(5)
  const json = (value) => {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify(value))
  }
  if (url.pathname.startsWith('/api/')) {
    if (endpoint === 'session') return json(session(req.headers.cookie?.includes('fixture=signed')))
    if (endpoint === 'login') {
      let body = ''
      for await (const chunk of req) body += chunk
      if (JSON.parse(body).password !== 'demo-password') {
        res.writeHead(401)
        return res.end('Wrong password.')
      }
      res.setHeader('Set-Cookie', 'fixture=signed; Path=/; HttpOnly; SameSite=Strict')
      return json({ signed_in: true })
    }
    if (endpoint === 'logout') {
      res.setHeader('Set-Cookie', 'fixture=; Max-Age=0; Path=/')
      return json({ signed_in: false })
    }
    if (endpoint === 'action' || endpoint === 'pair') {
      if (!req.headers.cookie?.includes('fixture=signed')) {
        res.writeHead(401)
        return res.end('Sign in first.')
      }
      if (endpoint === 'action') return json({ id: 'fixture-job', title: 'Synthetic action' })
    }
    if (endpoint === 'job')
      return json({
        title: 'Synthetic action',
        output: 'Completed synthetic action. No host was changed.\n',
        next: 60,
        done: true,
        ok: true,
      })
    if (endpoint === 'logs') {
      res.writeHead(200, { 'Content-Type': 'text/plain' })
      res.write('database ready\nGET /health HTTP healthcheck\n')
      if (url.searchParams.has('follow')) {
        const timer = setInterval(() => res.write('stream heartbeat\n'), 300)
        req.on('close', () => clearInterval(timer))
        return
      }
      return res.end('application connected\n')
    }
    if (fixtures[endpoint]) return json(fixtures[endpoint])
    res.writeHead(404)
    return res.end('Not found')
  }
  try {
    const relative = url.pathname === '/' ? 'index.html' : decodeURIComponent(url.pathname.slice(1))
    if (relative.includes('..')) throw new Error()
    const data = await readFile(root + relative)
    const ext = relative.split('.').pop()
    res.writeHead(200, {
      'Content-Type':
        {
          html: 'text/html',
          js: 'text/javascript',
          css: 'text/css',
          svg: 'image/svg+xml',
          ico: 'image/x-icon',
          png: 'image/png',
        }[ext] || 'application/octet-stream',
    })
    res.end(data)
  } catch {
    res.writeHead(404)
    res.end('Not found')
  }
}).listen(Number(process.env.PORT || 4178), '127.0.0.1', () =>
  console.log('Synthetic dashboard: http://127.0.0.1:' + (process.env.PORT || 4178))
)
