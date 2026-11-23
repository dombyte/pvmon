import { createRouter, createWebHistory, type Router } from 'vue-router'
import MorePage from '@/pages/MorePage.vue'
import OverviewPage from '@/pages/OverviewPage.vue'
import PlaceholderPage from '@/pages/PlaceholderPage.vue'
import { useSessionStore } from '@/stores/session'

export function createAppRouter(): Router {
  const router = createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/', name: 'overview', component: OverviewPage, meta: { titleKey: 'nav.overview' } },
      {
        path: '/day',
        name: 'day',
        component: () => import('@/pages/DayPage.vue'),
        meta: { titleKey: 'nav.day' },
      },
      {
        path: '/history',
        name: 'history',
        component: () => import('@/pages/HistoryPage.vue'),
        meta: { titleKey: 'nav.history' },
      },
      {
        path: '/reports',
        name: 'reports',
        component: () => import('@/pages/ReportsPage.vue'),
        meta: { titleKey: 'nav.reports' },
      },
      {
        path: '/devices',
        name: 'devices',
        component: () => import('@/pages/DevicesPage.vue'),
        meta: { titleKey: 'nav.devices' },
      },
      {
        path: '/devices/:id',
        name: 'device',
        component: () => import('@/pages/DevicePage.vue'),
        meta: { titleKey: 'nav.devices' },
      },
      {
        path: '/status',
        name: 'status',
        component: () => import('@/pages/StatusPage.vue'),
        meta: { titleKey: 'nav.status' },
      },
      {
        path: '/settings',
        name: 'settings',
        component: () => import('@/pages/SettingsPage.vue'),
        meta: { titleKey: 'nav.settings', admin: true },
      },
      {
        path: '/wizard',
        name: 'wizard',
        component: () => import('@/pages/WizardPage.vue'),
        meta: { titleKey: 'wizard.title', admin: true },
      },
      {
        path: '/login',
        name: 'login',
        component: () => import('@/pages/LoginPage.vue'),
        meta: { titleKey: 'login.title', bare: true },
      },
      {
        path: '/setup',
        name: 'setup',
        component: () => import('@/pages/SetupPage.vue'),
        meta: { titleKey: 'setup.title', bare: true },
      },
      { path: '/more', name: 'more', component: MorePage, meta: { titleKey: 'nav.more' } },
      {
        path: '/:rest(.*)*',
        name: 'not-found',
        component: PlaceholderPage,
        meta: { titleKey: 'pages.notFound' },
      },
    ],
  })
  router.beforeEach(guard)
  return router
}

interface Target {
  name?: unknown
  fullPath: string
  meta: Record<PropertyKey, unknown>
}

interface Access {
  setupRequired: boolean
  loggedIn: boolean
  canRead: boolean
}

/**
 * Where a navigation goes instead (true = where it wanted): setup while no user exists,
 * login when data is not public or the page is for admins (spec 9.1, 11.14).
 */
export function redirectFor(to: Target, a: Access) {
  if (a.setupRequired) return to.name === 'setup' ? true : { name: 'setup' }
  if (to.name === 'setup') return { name: 'overview' }
  if (to.name === 'login') return a.loggedIn ? { name: 'overview' } : true
  if (!a.loggedIn && (to.meta.admin === true || !a.canRead)) {
    return { name: 'login', query: { next: to.fullPath } }
  }
  return true
}

/** Without an answer from the server the pages load and show their own errors. */
async function guard(to: Target) {
  const session = useSessionStore()
  if (!session.ready) {
    try {
      await session.load()
    } catch {
      return true
    }
  }
  return redirectFor(to, {
    setupRequired: session.setupRequired,
    loggedIn: session.user !== null,
    canRead: session.canRead,
  })
}
