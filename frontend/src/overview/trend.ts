// Daily PV vs consumption for the overview trend (desktop, 14 days) and the last-days
// list (phone, 7 days), spec 11.4. Pure: driven by the energy answer only.
import type { EnergySeries, EnergyValue, SiteModel } from '@/api/types'
import { addDays, type LocalDate } from '@/format/localdate'
import { numberOf } from '@/energy/rows'

/** Days of the desktop chart; the phone list shows the newest `listDays` of them. */
export const trendDays = 14
export const listDays = 7

export interface DayPoint {
  date: LocalDate
  /** Today: still running, so no report is offered. */
  running: boolean
  /** Wh or null when the day has no data; pv is null on sites without PV. */
  pv: number | null
  consumption: number | null
  gridIn: number | null
  gridOut: number | null
  /** Self-sufficiency in percent. */
  selfSufficiency: number | null
}

/** The bar series of the trend; "pv" only on sites with PV. */
export type TrendKey = 'pv' | 'consumption'

const seriesOf: Record<keyof Omit<DayPoint, 'date' | 'running'>, string> = {
  pv: 'site/pv',
  consumption: 'site/consumption',
  gridIn: 'site/grid_in',
  gridOut: 'site/grid_out',
  selfSufficiency: 'site/self_sufficiency',
}

/** Series the trend needs for a site. */
export function trendSeries(site: SiteModel): string[] {
  const hasPV = site.site_series.includes(seriesOf.pv)
  return Object.values(seriesOf).filter((s) => hasPV || s !== seriesOf.pv)
}

/** The trend's bar keys for a site. */
export function trendKeys(site: SiteModel): TrendKey[] {
  return site.site_series.includes(seriesOf.pv) ? ['pv', 'consumption'] : ['consumption']
}

/** First day of a range of n days ending today. */
export function rangeStart(today: LocalDate, n: number): LocalDate {
  return addDays(today, 1 - n)
}

/**
 * One point per day from `from` to `today`, oldest first. Values are matched by period,
 * not by position, so a short or reordered answer never shifts a day.
 */
export function dayPoints(
  series: Record<string, EnergySeries>,
  from: LocalDate,
  today: LocalDate,
): DayPoint[] {
  const lookup = (name: string): Map<string, EnergyValue> =>
    new Map((series[name]?.values ?? []).map((v) => [v.period, v]))
  const maps = Object.fromEntries(
    Object.entries(seriesOf).map(([k, name]) => [k, lookup(name)]),
  ) as Record<keyof typeof seriesOf, Map<string, EnergyValue>>
  const out: DayPoint[] = []
  for (let d = from; d <= today; d = addDays(d, 1)) {
    const at = (k: keyof typeof seriesOf) => numberOf(maps[k].get(d))
    out.push({
      date: d,
      running: d === today,
      pv: at('pv'),
      consumption: at('consumption'),
      gridIn: at('gridIn'),
      gridOut: at('gridOut'),
      selfSufficiency: at('selfSufficiency'),
    })
  }
  return out
}

/** Largest value of the given keys over all points; 0 when nothing has data. */
export function maxOf(points: DayPoint[], keys: TrendKey[]): number {
  let max = 0
  for (const p of points) for (const k of keys) max = Math.max(max, p[k] ?? 0)
  return max
}

const fullPercent = 100

/** Bar length in percent of max (0..100); missing values and an empty scale give 0. */
export function share(value: number | null, max: number): number {
  if (value === null || max <= 0) return 0
  return Math.min(fullPercent, Math.max(0, (value / max) * fullPercent))
}

/** Sum of a key over the points that have data, null when none has. */
export function sumOf(points: DayPoint[], key: TrendKey): number | null {
  const known = points.map((p) => p[key]).filter((v): v is number => v !== null)
  return known.length ? known.reduce((a, b) => a + b, 0) : null
}
