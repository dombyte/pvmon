// Day view data (spec 11.3): 15-minute powers from the energy buckets (W = Wh × 4, exact
// for every site quantity) plus the PV min/max band and the SoC from the gauges. Pure.
import type { EnergySeries, GaugePoint, GaugeSeries, SiteModel } from '@/api/types'
import { numberOf } from '@/energy/rows'

const slotsPerHour = 4

/** Series names the day view requests for a site. */
export interface DayRequest {
  energy: string[]
  gauge: string[]
  /** Loadpoint categories with their own line, in site order. */
  categories: string[]
  /** Loadpoints of categories with several meters, which can be shown one by one. */
  meters: LoadMeter[]
}

/** A top-level consumer meter that can get its own line (from its power gauge). */
interface LoadMeter {
  id: string
  category: string
  label: string
}

/** Top-level consumers of categories that have more than one, with a power series. */
function splitMeters(site: SiteModel, categories: string[]): LoadMeter[] {
  const all = site.meters.filter(
    (m) =>
      m.role === 'consumer' &&
      (!m.parent || m.parent === 'site') &&
      !!m.category &&
      categories.includes(m.category),
  )
  return all
    .filter((m) => all.filter((o) => o.category === m.category).length > 1)
    .filter((m) => m.series.includes(`${m.id}/power`))
    .map((m) => ({ id: m.id, category: m.category ?? '', label: m.label }))
}

const has = (site: SiteModel, name: string) => site.site_series.includes(name)

export function dayRequest(site: SiteModel): DayRequest {
  const categories = site.site_series
    .filter((s) => s.startsWith('site/loadpoints/'))
    .map((s) => s.slice('site/loadpoints/'.length))
  const energy = [
    'site/pv',
    'site/consumption',
    'site/house_rest',
    'site/grid_in',
    'site/grid_out',
    'site/battery_in',
    'site/battery_out',
  ].filter((n) => has(site, n))
  energy.push(...categories.map((c) => `site/loadpoints/${c}`))
  const gauge: string[] = []
  if (has(site, 'site/pv')) gauge.push('site/pv')
  if (has(site, 'site/soc')) gauge.push('site/soc')
  const meters = splitMeters(site, categories)
  gauge.push(...meters.map((m) => `${m.id}/power`))
  return { energy, gauge, categories, meters }
}

/** One 15-minute slot; every power in W, null without data. */
export interface DaySlot {
  /** Slot start as the API keys it (RFC 3339, server time). */
  at: string
  /** "HH:MM" in server time. */
  time: string
  pv: number | null
  pvMin: number | null
  pvMax: number | null
  consumption: number | null
  /** Consumption without the loadpoints (can be negative: a balance difference). */
  house: number | null
  loadpoints: Record<string, number | null>
  /** + import, − export. */
  grid: number | null
  /** + discharge, − charge. */
  battery: number | null
  soc: number | null
}

type Lookup = (name: string, at: string) => number | null

function energyLookup(series: Record<string, EnergySeries>): Lookup {
  const maps = new Map(
    Object.values(series).map((s) => [s.name, new Map(s.values.map((v) => [v.period, v]))]),
  )
  return (name, at) => {
    const wh = numberOf(maps.get(name)?.get(at))
    return wh === null ? null : wh * slotsPerHour
  }
}

/** Difference a − b of two powers; null only when both are missing (a site without one). */
function net(a: number | null, b: number | null): number | null {
  if (a === null && b === null) return null
  return (a ?? 0) - (b ?? 0)
}

/** The slots of the day, in the order the energy answer lists them (92, 96 or 100). */
export function daySlots(
  energy: Record<string, EnergySeries>,
  gauge: Record<string, GaugeSeries>,
  req: DayRequest,
): DaySlot[] {
  const first = req.energy[0]
  const periods = first ? (energy[first]?.values.map((v) => v.period) ?? []) : []
  const e = energyLookup(energy)
  const g = (name: string) => new Map((gauge[name]?.points ?? []).map((p) => [p.at, p]))
  const pvG = g('site/pv')
  const socG = g('site/soc')
  const meterG = new Map(req.meters.map((m) => [m.id, g(`${m.id}/power`)]))
  const pick = (m: Map<string, GaugePoint>, at: string, k: 'avg' | 'min' | 'max') =>
    m.get(at)?.[k] ?? null
  return periods.map((at) => ({
    at,
    time: at.slice(11, 16),
    // The PV line uses the power average of the band's own gauge: coarse counters (0.1 kWh)
    // book in steps and would leave the band; the counters are the fallback.
    pv: pick(pvG, at, 'avg') ?? e('site/pv', at),
    pvMin: pick(pvG, at, 'min'),
    pvMax: pick(pvG, at, 'max'),
    consumption: e('site/consumption', at),
    house: e('site/house_rest', at),
    // Categories by name, single meters as "m:<id>" (their power average).
    loadpoints: Object.fromEntries([
      ...req.categories.map((c) => [c, e(`site/loadpoints/${c}`, at)]),
      ...req.meters.map((m) => [`m:${m.id}`, pick(meterG.get(m.id)!, at, 'avg')]),
    ]),
    grid: net(e('site/grid_in', at), e('site/grid_out', at)),
    battery: net(e('site/battery_out', at), e('site/battery_in', at)),
    soc: pick(socG, at, 'avg'),
  }))
}

/** Slots with consumption data (the balance of every source) of all slots. */
export function dayQuality(slots: DaySlot[]): { complete: number; total: number } {
  return { complete: slots.filter((s) => s.consumption !== null).length, total: slots.length }
}
