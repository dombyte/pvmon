// Live power per flow role, derived from the site model and live values (spec 11.5, 11.6).
// Pure: the site model and a value lookup in, numbers out; missing stays undefined.
import type { SiteModel } from '@/api/types'

export const categories = ['wallbox', 'heatpump', 'heater', 'other'] as const
type Category = (typeof categories)[number]
export type NodeKind = 'pv' | 'grid' | 'battery' | 'house' | Category

/** Returns the numeric live value of a key, undefined when missing or not a number. */
export type Lookup = (key: string) => number | undefined

/** Live key of a site quantity (spec 9.3). */
export function liveKey(quantity: string): string {
  return quantity === 'site/soc' ? quantity : `${quantity}.power`
}

/** One flow node: signed power (sources positive towards the house), meters behind it. */
export interface RolePower {
  kind: NodeKind
  /** Number of meters that make up the node (PV sources, wallboxes); 0 for House. */
  count: number
  /** For pv/consumers: power; grid: import − export; battery: discharge − charge. */
  power: number | undefined
  /** Meter IDs for the breakdown: the node's meters; for House its backup outputs. */
  meters: string[]
}

export interface PowerState {
  nodes: RolePower[]
  soc: number | undefined
  socApprox: boolean
}

const consumerOrder: NodeKind[] = ['house', ...categories]

/** Every live key the overview needs: site quantities plus the power of each meter. */
export function overviewKeys(site: SiteModel): string[] {
  const keys = site.site_series.map(liveKey)
  for (const m of site.meters) if (m.series.includes(`${m.id}/power`)) keys.push(`${m.id}/power`)
  return [...new Set(keys)].sort()
}

function metersOf(site: SiteModel, kind: NodeKind): string[] {
  return site.meters
    .filter((m) => {
      // Like evcc's home power, House is consumption − loadpoints; a backup output is a
      // part of it, wherever the loadpoints are wired (spec 2, 11.5).
      if (kind === 'house') return m.role === 'backup'
      if (m.role === 'consumer') return m.parent === 'site' && m.category === kind
      // PV strings below a PV total are part of it (spec 2).
      if (m.role === 'pv') return kind === 'pv' && (m.parent ?? 'site') === 'site'
      return m.role === kind
    })
    .map((m) => m.id)
}

function diff(a: number | undefined, b: number | undefined): number | undefined {
  return a === undefined || b === undefined ? undefined : a - b
}

function nodePower(kind: NodeKind, get: Lookup): number | undefined {
  const q = (name: string) => get(liveKey(`site/${name}`))
  switch (kind) {
    case 'pv':
      return q('pv')
    case 'grid':
      return diff(q('grid_in'), q('grid_out'))
    case 'battery':
      return diff(q('battery_out'), q('battery_in'))
    case 'house':
      return q('house_rest')
    default:
      return q(`loadpoints/${kind}`)
  }
}

function present(site: SiteModel, kind: NodeKind): boolean {
  const c = site.capabilities
  switch (kind) {
    case 'pv':
      return c.pv > 0
    case 'grid':
      return c.grid
    case 'battery':
      return c.battery > 0
    case 'house':
      return true
    default:
      return (c.loadpoints[kind] ?? 0) > 0
  }
}

/** Nodes in fixed order: sources (PV, grid, battery), then the consumer column. */
export function powerState(site: SiteModel, get: Lookup): PowerState {
  const kinds: NodeKind[] = ['pv', 'grid', 'battery', ...consumerOrder]
  const nodes = kinds
    .filter((k) => present(site, k))
    .map((kind) => {
      const meters = metersOf(site, kind)
      const count = kind === 'house' ? 0 : meters.length
      return { kind, count, power: nodePower(kind, get), meters }
    })
  const socApprox = site.meters
    .filter((m) => m.role === 'battery')
    .some((m) => !m.capacity_wh)
  return { nodes, soc: get('site/soc'), socApprox }
}

/** True for nodes of the consumer column. */
export function isConsumer(kind: NodeKind): boolean {
  return consumerOrder.includes(kind)
}

/** Texts for meterLabel. */
export interface MeterLabelText {
  backup: string
  ofWhich: (name: string) => string
}

/**
 * Breakdown label of a meter: its label, falling back to the ID. A backup output is part
 * of House and reads "of which <label or Backup>".
 */
export function meterLabel(site: SiteModel, id: string, text: MeterLabelText): string {
  const m = site.meters.find((x) => x.id === id)
  const named = m && m.label !== m.id ? m.label : undefined
  if (m?.role === 'backup') return text.ofWhich(named ?? text.backup)
  return named ?? id
}

/** Direction of a battery or grid value in words, as the "Power now" rows show it. */
export type Direction = 'charging' | 'discharging' | 'import' | 'export'

/**
 * A breakdown value as magnitude and direction: battery and grid are signed by role
 * (battery > 0 discharging, grid > 0 import; spec 2), which the breakdown spells out
 * instead of showing a bare minus sign. Other roles stay as they are.
 */
export function directed(
  kind: NodeKind,
  w: number | undefined,
): { power: number | undefined; dir: Direction | null } {
  if (w === undefined || w === 0 || (kind !== 'battery' && kind !== 'grid')) {
    return { power: w, dir: null }
  }
  const dir: Direction =
    kind === 'battery' ? (w > 0 ? 'discharging' : 'charging') : w > 0 ? 'import' : 'export'
  return { power: Math.abs(w), dir }
}
