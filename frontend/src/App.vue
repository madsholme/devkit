<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  FolderGit2,
  Container,
  Monitor,
  GitBranch,
  History,
  Eye,
  RefreshCw,
  Settings,
  LogIn,
  LogOut,
  Palette,
  Cpu,
  MemoryStick,
  HardDrive,
  Activity,
  ExternalLink,
  QrCode,
  Server,
} from 'lucide-vue-next'
import { TopNavigation, MobileNavigation, SkipLink, type NavGroup } from '@madsholme/ui/shell'
import { useThemeState } from '@madsholme/ui/theme'
import { themes, type ThemePreference } from '@madsholme/themes'
import { Button } from '@madsholme/ui/button'
import { Badge } from '@madsholme/ui/badge'
import { Progress } from '@madsholme/ui/progress'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from '@madsholme/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@madsholme/ui/tooltip'
import {
  state,
  tabs,
  currentSample,
  startPolling,
  refresh,
  openDialog,
  signOut,
  pairDevice,
  type Tab,
} from './dashboard'
import { bytes, percent, safeURL, store } from './format'
import { themeController } from './theme'
import InventoryNotice from './components/InventoryNotice.vue'
import DashboardDialogs from './components/DashboardDialogs.vue'
const route = useRoute()
const router = useRouter()
const mobileOpen = ref(false)
const theme = useThemeState(themeController)
const pages = {
  projects: {
    title: 'Projects',
    description: 'Your checkouts, worktrees and runtimes, together.',
    icon: FolderGit2,
  },
  previews: {
    title: 'Previews',
    description: 'Live workspaces and isolated review environments.',
    icon: Eye,
  },
  containers: {
    title: 'Containers',
    description: 'Every Docker container on this host.',
    icon: Container,
  },
  vms: {
    title: 'Virtual machines',
    description: 'QEMU and libvirt guests, resources and private connections.',
    icon: Monitor,
  },
  worktrees: {
    title: 'Worktrees',
    description: 'Git checkouts, source changes and running previews.',
    icon: GitBranch,
  },
  history: {
    title: 'History',
    description: 'Activity from the CLI, dashboard and automatic maintenance.',
    icon: History,
  },
}
const page = computed(() => pages[state.tab])
const navigation = computed<NavGroup[]>(() =>
  tabs.map((id) => ({
    id,
    label:
      pages[id].title +
      (id === 'history' && failureCount.value
        ? ` · ${failureCount.value} ${failureCount.value === 1 ? 'failure' : 'failures'}`
        : ''),
    to: `/${id}`,
    icon: pages[id].icon,
  }))
)
watch(
  () => route.path,
  (path) => {
    const tab = path.slice(1) as Tab
    if (tabs.includes(tab)) {
      state.tab = tab
      store.set('tab', tab)
      mobileOpen.value = false
      void refresh()
    }
  },
  { immediate: true }
)
watch(
  () => state.overview?.host.name,
  (name) => (document.title = `${name || 'devkit'} · devkit`)
)
const sample = currentSample
const metrics = computed(() => [
  {
    label: 'CPU',
    icon: Cpu,
    value: sample.value ? `${sample.value.cpu.toFixed(0)}%` : '—',
    pct: sample.value?.cpu || 0,
  },
  {
    label: 'Memory',
    icon: MemoryStick,
    value: sample.value
      ? `${bytes(sample.value.mem_used)} / ${bytes(sample.value.mem_total)}`
      : '—',
    pct: sample.value ? percent(sample.value.mem_used, sample.value.mem_total) : 0,
  },
  {
    label: 'Disk',
    icon: HardDrive,
    value: sample.value
      ? `${percent(sample.value.disk_used, sample.value.disk_total).toFixed(0)}%`
      : '—',
    pct: sample.value ? percent(sample.value.disk_used, sample.value.disk_total) : 0,
  },
])
const failureCount = computed(
  () => state.history.filter((e) => !e.ok && Date.now() - Date.parse(e.time) < 86400000).length
)
const reclaimable = computed(() =>
  (state.overview?.host.disk || []).reduce((total, row) => {
    const match = /([\d.]+)\s*([kMGT]?)B/.exec(row.reclaimable)
    return (
      total +
      (match ? Number(match[1]) * ({ '': 1, k: 1e3, M: 1e6, G: 1e9, T: 1e12 }[match[2]!] || 0) : 0)
    )
  }, 0)
)
const busy = computed(() => Object.values(state.busy).some(Boolean))
const notices = computed(() => [
  ...new Set(
    [
      state.error,
      state.errors.session,
      state.errors.overview,
      state.errors.resources,
      state.errors[state.tab],
    ].filter(Boolean)
  ),
])
async function shortcut(event: KeyboardEvent) {
  if (
    event.key === '/' &&
    !state.dialog &&
    !(event.target as HTMLElement).closest('input,textarea,select,[contenteditable]')
  ) {
    event.preventDefault()
    await router.push('/previews')
    await nextTick()
    document.querySelector<HTMLInputElement>('#preview-search')?.focus()
  }
}
function focusMain() {
  document.getElementById('main-content')?.focus()
}
let stop = () => {}
onMounted(() => {
  stop = startPolling()
  document.addEventListener('keydown', shortcut)
})
onBeforeUnmount(() => {
  stop()
  document.removeEventListener('keydown', shortcut)
})
</script>
<template>
  <TooltipProvider
    ><div class="sd-app-shell">
      <SkipLink @click.prevent="focusMain" />
      <header class="site-header">
        <div class="shell-container header-inner">
          <RouterLink to="/projects" class="brand" aria-label="devkit projects"
            ><img src="/logo.svg" alt="" /><span>devkit</span></RouterLink
          >
          <div class="desktop-nav">
            <TopNavigation :navigation="navigation" :current-id="state.tab" />
          </div>
          <div class="mobile-nav">
            <MobileNavigation
              v-model:open="mobileOpen"
              :navigation="navigation"
              :current-id="state.tab"
            />
          </div>
          <div class="header-spacer" />
          <Button
            v-if="!state.session.signed_in && state.session.password_set"
            variant="ghost"
            size="sm"
            data-site-header-interactive
            @click="openDialog('signin')"
            ><LogIn :size="15" /><span class="signin-label">Sign in</span></Button
          ><DropdownMenu :modal="false"
            ><DropdownMenuTrigger as-child
              ><Button
                variant="ghost"
                size="icon"
                aria-label="Settings"
                data-site-header-interactive
                ><Settings :size="18" /></Button></DropdownMenuTrigger
            ><DropdownMenuContent align="end" class="settings-menu"
              ><DropdownMenuLabel>{{
                state.session.signed_in ? 'Signed in to this host' : 'Read-only access'
              }}</DropdownMenuLabel>
              <p v-if="!state.session.password_set" class="menu-help">
                Run <code>devkit web password</code> on the host to enable actions.
              </p>
              <DropdownMenuSeparator /><DropdownMenuSub
                ><DropdownMenuSubTrigger><Palette :size="16" />Theme</DropdownMenuSubTrigger
                ><DropdownMenuSubContent class="theme-menu"
                  ><DropdownMenuRadioGroup
                    :model-value="theme.preference.value"
                    @update:model-value="theme.select($event as ThemePreference)"
                    ><DropdownMenuRadioItem value="system">Match system</DropdownMenuRadioItem
                    ><DropdownMenuRadioItem
                      v-for="choice in themes"
                      :key="choice.key"
                      :value="choice.key"
                      >{{ choice.label }}</DropdownMenuRadioItem
                    ></DropdownMenuRadioGroup
                  ></DropdownMenuSubContent
                ></DropdownMenuSub
              ><DropdownMenuItem
                v-if="state.session.signed_in"
                :disabled="state.pending"
                @select="signOut"
                ><LogOut :size="16" />Sign out</DropdownMenuItem
              ><DropdownMenuItem
                v-else-if="state.session.password_set"
                @select="openDialog('signin')"
                ><LogIn :size="16" />Sign in</DropdownMenuItem
              ></DropdownMenuContent
            ></DropdownMenu
          >
        </div>
      </header>
      <main id="main-content" tabindex="-1" class="shell-container operator-workspace">
        <div class="host-line">
          <div class="host-identity">
            <Server :size="16" /><strong>{{ state.overview?.host.name || 'devkit host' }}</strong
            ><span class="muted">{{ state.overview?.host.domain }}</span>
          </div>
          <div class="host-services">
            <template v-for="service in state.overview?.host.services || []" :key="service.name"
              ><Tooltip
                ><TooltipTrigger as-child
                  ><a
                    v-if="safeURL(service.url)"
                    :href="safeURL(service.url)"
                    target="_blank"
                    rel="noopener"
                    class="service-link"
                    ><span
                      class="status-dot"
                      :class="service.status === 'up' ? 'is-up' : 'is-down'" />{{ service.name
                    }}<ExternalLink :size="12" /></a
                  ><span v-else tabindex="0" class="service-link"
                    ><span
                      class="status-dot"
                      :class="service.status === 'up' ? 'is-up' : 'is-down'"
                    />{{ service.name }}</span
                  ></TooltipTrigger
                ><TooltipContent
                  ><p>{{ service.name }} · {{ service.status }}</p>
                  <p>{{ service.detail }}</p>
                  <p v-if="service.name === 'Traefik'">
                    Certificate: {{ state.overview?.host.certificate_expires }}
                  </p></TooltipContent
                ></Tooltip
              ><Badge
                v-if="service.name === 'Traefik' && (state.overview!.host.certificate_days == null || state.overview!.host.certificate_days < 21)"
                variant="warning"
                >Certificate
                {{
                  state.overview!.host.certificate_days == null
                    ? 'unavailable'
                    : state.overview!.host.certificate_days < 0
                    ? 'expired'
                    : `expires in ${state.overview!.host.certificate_days}d`
                }}</Badge
              ></template
            ><Button
              v-if="
                state.session.signed_in &&
                state.session.pair &&
                state.overview?.host.services.some((s) => s.name === 'T3 Code' && s.status === 'up')
              "
              variant="ghost"
              size="sm"
              @click="pairDevice"
              ><QrCode :size="15" />Pair device</Button
            >
          </div>
        </div>
        <button
          class="system-summary"
          aria-haspopup="dialog"
          aria-label="Open system monitor"
          @click="openDialog('system')"
        >
          <span v-for="metric in metrics" :key="metric.label" class="summary-metric"
            ><component :is="metric.icon" :size="17" /><span class="summary-label">{{
              metric.label
            }}</span
            ><strong>{{ metric.value }}</strong
            ><Progress
              :model-value="Math.min(100, metric.pct)"
              :aria-label="`${metric.label} usage`"
              :color="
                metric.pct >= 90 ? 'destructive' : metric.pct >= 75 ? 'warning' : 'primary'
              " /></span
          ><span class="summary-metric containers-summary"
            ><Container :size="17" /><span class="summary-label">Running containers</span
            ><strong>{{ state.overview?.host.running_containers ?? '—' }}</strong></span
          ><Activity :size="17" class="monitor-icon" />
        </button>
        <p v-if="reclaimable >= 5e9" class="note">
          {{ (reclaimable / 1e9).toFixed(1) }} GB of Docker data reclaimable
        </p>
        <header class="workspace-heading">
          <div>
            <h1>
              <component :is="page.icon" :size="25" />{{ page.title
              }}<Badge v-if="state.tab === 'history' && failureCount" variant="destructive"
                >{{ failureCount }} recent failures</Badge
              >
            </h1>
            <p>{{ page.description }}</p>
          </div>
          <div class="heading-actions">
            <span v-if="state.updated[state.tab] || state.updated.overview" class="updated"
              >Updated {{ state.updated[state.tab] || state.updated.overview }}</span
            ><Button variant="outline" :disabled="busy" @click="refresh"
              ><RefreshCw :size="15" :class="{ 'animate-spin': busy }" />Refresh</Button
            >
          </div>
        </header>
        <InventoryNotice v-for="notice in notices" :key="notice" :text="notice" error /><RouterView
          v-slot="{ Component }"
          ><KeepAlive><component :is="Component" /></KeepAlive
        ></RouterView>
        <footer class="page-footer">
          <span
            >devkit
            <a
              v-if="state.overview?.host.version"
              :href="`https://github.com/steenfeldt/devkit/commit/${encodeURIComponent(
                state.overview.host.commit
              )}`"
              target="_blank"
              rel="noopener"
              >{{ state.overview.host.version }}</a
            ></span
          ><span
            >{{ state.session.signed_in ? 'Signed in' : 'Read-only'
            }}<span class="footer-separator">·</span
            ><a href="https://github.com/steenfeldt/devkit" target="_blank" rel="noopener"
              >GitHub</a
            ></span
          >
        </footer>
      </main>
      <DashboardDialogs /></div
  ></TooltipProvider>
</template>
