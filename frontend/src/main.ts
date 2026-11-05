import { createApp } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import App from './App.vue'
import ProjectsView from './views/ProjectsView.vue'
import ProjectDetailView from './views/ProjectDetailView.vue'
import PreviewsView from './views/PreviewsView.vue'
import ContainersView from './views/ContainersView.vue'
import VMsView from './views/VMsView.vue'
import WorktreesView from './views/WorktreesView.vue'
import HistoryView from './views/HistoryView.vue'
import { store } from './format'
import { tabs } from './dashboard'
import './theme'
import './main.css'
// Old shared links (#vms, #projects) remain accepted alongside Vue Router links.
function normalizeHash() {
  if (/^#(projects|previews|containers|vms|worktrees|history)$/.test(location.hash))
    history.replaceState(null, '', `#/${location.hash.slice(1)}`)
}
normalizeHash()
window.addEventListener('hashchange', normalizeHash)
const saved = store.get('tab')
const initial = tabs.includes(saved as (typeof tabs)[number]) ? saved : 'projects'
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: `/${initial}` },
    { path: '/projects', component: ProjectsView },
    { path: '/projects/:project/:section?', name: 'project', component: ProjectDetailView },
    { path: '/previews', component: PreviewsView },
    { path: '/containers', component: ContainersView },
    { path: '/vms', component: VMsView },
    { path: '/worktrees', component: WorktreesView },
    { path: '/history', component: HistoryView },
    { path: '/:pathMatch(.*)*', redirect: '/projects' },
  ],
})
createApp(App).use(router).mount('#app')
