// Energy per period for the history chart, its detail card and the range sum (spec 11.8).
// Pure: driven by the energy answer only; values are matched by period key.
import type { EnergySeries, EnergyValue, SiteModel } from '@/api/types'
import type { Tokens } from '@/charts/tokens'
import { missingDays, numberOf } from '@/energy/rows'
import type { LocalDate } from '@/format/localdate'
import { periodKey, periods, type Range } from './range'

/** Bar series of the history; each only when the site has it. */
export type HistoryKey = 'pv' | 'consumption' | LoadKey | 'gridIn' | 'gridOut'
/** The parts of the consumption on a site with loadpoints. */
type LoadKey = 'house' | 'wallbox' | 'heatpump' | 'heater' | 'other'

const seriesOf: Record<HistoryKey | 'selfSufficiency', string> = {
  pv: 'site/pv',
  consumption: 'site/consumption',
  house: 'site/house_rest',
  wallbox: 'site/loadpoints/wallbox',
  heatpump: 'site/loadpoints/heatpump',
  heater: 'site/loadpoints/heater',
  other: 'site/loadpoints/other',
  gridIn: 'site/grid_in',
  gridOut: 'site/grid_out',
  selfSufficiency: 'site/self_sufficiency',
}

/** Colour token per key. */
export const tokenOf: Record<HistoryKey, keyof Tokens> = {
  pv: 'pv',
  consumption: 'home',
  house: 'home',
  wallbox: 'ev',
  heatpump: 'heatpump',
  heater: 'heater',
  other: 'other',
  gridIn: 'grid',
  gridOut: 'ink-2',
}

const loadpointKeys: LoadKey[] = ['wallbox', 'heatpump', 'heater', 'other']
const order: HistoryKey[] = ['pv', 'consumption', 'house', ...loadpointKeys, 'gridIn', 'gridOut']

/** Whether a key is a part of the consumption (stacked in the chart). */
export function isLoadPart(k: HistoryKey): boolean {
  return k === 'house' || (loadpointKeys as HistoryKey[]).includes(k)
}

/** The bar keys of a site, in fixed order. */
export function historyKeys(site: SiteModel): HistoryKey[] {
  const keys = order.filter((k) => site.site_series.includes(seriesOf[k]))
  // Without loadpoints the house equals the consumption.
  return keys.some((k) => k !== 'house' && isLoadPart(k)) ? keys : keys.filter((k) => k !== 'house')
}

/**
 * Keys with a toggle chip. With loadpoints the consumption is the sum of its parts: it
 * stays in the details and sums but has no bar of its own to toggle.
 */
export function chipKeys(keys: HistoryKey[]): HistoryKey[] {
  return keys.includes('house') ? keys.filter((k) => k !== 'consumption') : keys
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

