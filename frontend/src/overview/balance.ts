// Rows of the phone "Power now" card (spec 11.6): "from" (sources) and "to" (sinks), each
// one shared bar whose segments are shares of the row total.
import { categories, type PowerState, type NodeKind } from './power'

export interface Item {
  kind: NodeKind
  count: number
  power: number | undefined
  meters: string[]
}

export interface Row {
  items: Item[]
  /** Segments to draw: items with power > 0, share in percent of the row total. */
  segments: { kind: NodeKind; share: number }[]
  total: number
}

const percent = 100

function row(items: Item[]): Row {
  const total = items.reduce((s, i) => s + Math.max(i.power ?? 0, 0), 0)
  const segments =
    total > 0
      ? items
          .filter((i) => (i.power ?? 0) > 0)
          .map((i) => ({ kind: i.kind, share: ((i.power ?? 0) / total) * percent }))
      : []
  return { items, segments, total }
}

/**
 * Every item that can ever appear in each row (battery and grid in both): the card keeps
 * room for them, so its height does not change when the battery or grid turns around.
 */
export function capacity(state: PowerState): { from: Item[]; to: Item[] } {
  const swaps = (k: NodeKind) => k === 'battery' || k === 'grid'
  return {
    from: state.nodes.filter((n) => n.kind === 'pv' || swaps(n.kind)),
    to: state.nodes.filter((n) => n.kind !== 'pv'),
  }
}

/**
 * Battery and grid switch rows with their sign: discharging/import are sources,
 * charging/export are sinks; at 0 W (or missing) they stay listed under "from".
 */
export function balance(state: PowerState): { from: Row; to: Row } {
  const from: Item[] = []
  const to: Item[] = []
  const byKind = new Map(state.nodes.map((n) => [n.kind, n]))
  const pv = byKind.get('pv')
  if (pv) from.push({ ...pv })
  for (const kind of ['battery', 'grid'] as const) {
    const n = byKind.get(kind)
    if (!n) continue
    if ((n.power ?? 0) < 0) to.push({ ...n, power: -(n.power ?? 0) })
    else from.push({ ...n })
  }
  for (const kind of categories) {
    const n = byKind.get(kind)
    if (n) to.push({ ...n })
  }
  const house = byKind.get('house')
  if (house) to.push({ ...house })
  // Sinks in spec order: loadpoints, house, battery charging, grid export.
  const order = (k: NodeKind) => (k === 'battery' ? 1 : k === 'grid' ? 2 : 0)
  to.sort((a, b) => order(a.kind) - order(b.kind))
  return { from: row(from), to: row(to) }
}
