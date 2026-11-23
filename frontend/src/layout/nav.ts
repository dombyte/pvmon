import {
  Activity,
  CalendarDays,
  ChartColumn,
  Cpu,
  Ellipsis,
  FileText,
  LayoutDashboard,
  Settings,
} from '@lucide/vue'
import type { Component } from 'vue'

export interface NavItem {
  to: string
  labelKey: string
  icon: Component
  /** Route paths that highlight this item. */
  match: readonly string[]
}

const overview: NavItem = { to: '/', labelKey: 'nav.overview', icon: LayoutDashboard, match: ['/'] }
const history: NavItem = {
  to: '/history',
  labelKey: 'nav.history',
  icon: ChartColumn,
  match: ['/history'],
}
const reports: NavItem = {
  to: '/reports',
  labelKey: 'nav.reports',
  icon: FileText,
  match: ['/reports'],
}
const devices: NavItem = { to: '/devices', labelKey: 'nav.devices', icon: Cpu, match: ['/devices'] }
const day: NavItem = { to: '/day', labelKey: 'nav.day', icon: CalendarDays, match: ['/day'] }

export const desktopNav: readonly NavItem[] = [
  overview,
  day,
  history,
  reports,
  devices,
  { to: '/status', labelKey: 'nav.status', icon: Activity, match: ['/status'] },
  { to: '/settings', labelKey: 'nav.settings', icon: Settings, match: ['/settings'] },
]

// The daily pages sit in the bar; devices, status and settings are one tap further.
export const compactNav: readonly NavItem[] = [
  overview,
  day,
  history,
  reports,
  {
    to: '/more',
    labelKey: 'nav.more',
    icon: Ellipsis,
    match: ['/more', '/devices', '/status', '/settings'],
  },
]

/** Entries of the compact "More" menu (spec 11.3). */
export const moreNav: readonly NavItem[] = desktopNav.filter((i) =>
  ['/devices', '/status', '/settings'].includes(i.to),
)

export function isActive(item: NavItem, path: string): boolean {
  return item.match.some((m) => (m === '/' ? path === '/' : path === m || path.startsWith(`${m}/`)))
}
