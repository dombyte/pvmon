// Energy per period for the history chart, its detail card and the range sum (spec 11.8).
// Pure: driven by the energy answer only; values are matched by period key.
import type { EnergySeries, EnergyValue, SiteModel } from '@/api/types'
import { missingDays, numberOf } from '@/energy/rows'
import type { LocalDate } from '@/format/localdate'
import { periodKey, periods, type Range } from './range'

/** Bar series of the history; each only when the site has it. */
export type HistoryKey = 'pv' | 'consumption' | 'gridIn' | 'gridOut'

const seriesOf: Record<HistoryKey | 'selfSufficiency', string> = {
  pv: 'site/pv',
  consumption: 'site/consumption',
  gridIn: 'site/grid_in',
  gridOut: 'site/grid_out',
  selfSufficiency: 'site/self_sufficiency',
}

/** Colour token per key. */
export const tokenOf: Record<HistoryKey, 'pv' | 'home' | 'grid' | 'ev'> = {
  pv: 'pv',
  consumption: 'home',
  gridIn: 'grid',
  gridOut: 'ev',
}

const order: HistoryKey[] = ['pv', 'consumption', 'gridIn', 'gridOut']

/** The bar keys of a site, in fixed order. */
export function historyKeys(site: SiteModel): HistoryKey[] {
  return order.filter((k) => site.site_series.includes(seriesOf[k]))
}

/**
 * Series to request: the bars plus self-sufficiency, which every site has (derived from
 * consumption and grid; the live list site_series does not carry ratios).
 */
export function historySeries(site: SiteModel): string[] {
  return [...historyKeys(site).map((k) => seriesOf[k]), seriesOf.selfSufficiency]
}

export interface PeriodPoint {
  /** First day of the period. */
  start: LocalDate
  /** Wh per key, null without data. */
  values: Record<HistoryKey, number | null>
  selfSufficiency: number | null
  /** Days of the period with missing data, sorted. */
  missing: string[]
  estimated: boolean
}

/** One point per period of the range, oldest first. */
export function periodPoints(series: Record<string, EnergySeries>, r: Range): PeriodPoint[] {
  const maps = Object.fromEntries(
    Object.entries(seriesOf).map(([k, name]) => [
      k,
      new Map((series[name]?.values ?? []).map((v) => [v.period, v])),
    ]),
  ) as Record<keyof typeof seriesOf, Map<string, EnergyValue>>
  return periods(r).map((start) => {
    const key = periodKey(r.gran, start)
    const at = (k: keyof typeof seriesOf) => maps[k].get(key)
    const values = Object.fromEntries(order.map((k) => [k, numberOf(at(k))])) as PeriodPoint['values']
    const all = order.map(at).filter((v): v is EnergyValue => v !== undefined)
    return {
      start,
      values,
      selfSufficiency: numberOf(at('selfSufficiency')),
      missing: missingDays(all),
      estimated: all.some((v) => v.estimated),
    }
  })
}

export interface RangeSum {
  values: Record<HistoryKey, number | null>
  /** Self-sufficiency of the summed energies, null without consumption. */
  selfSufficiency: number | null
  /** Periods without any data, of all periods. */
  empty: number
  total: number
}

const fullPercent = 100

/** Sums of the range over the periods with data. */
export function rangeSum(points: PeriodPoint[], keys: HistoryKey[]): RangeSum {
  const values = Object.fromEntries(order.map((k) => [k, null])) as RangeSum['values']
  for (const p of points) {
    for (const k of keys) {
      const v = p.values[k]
      if (v !== null) values[k] = (values[k] ?? 0) + v
    }
  }
  const cons = values.consumption
  const selfSufficiency =
    cons && cons > 0 && values.gridIn !== null
      ? Math.min(fullPercent, Math.max(0, (fullPercent * (cons - values.gridIn)) / cons))
      : null
  const empty = points.filter((p) => keys.every((k) => p.values[k] === null)).length
  return { values, selfSufficiency, empty, total: points.length }
}

