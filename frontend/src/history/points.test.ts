import { describe, expect, it } from 'vitest'
import type { EnergySeries, SiteModel } from '@/api/types'
import { chipKeys, historyKeys, historySeries, periodPoints, rangeSum } from './points'
import { latest } from './range'

const site = (series: string[]) => ({ site_series: series }) as unknown as SiteModel

const energy = (name: string, values: [string, number | null, string[]?][]): EnergySeries => ({
  name,
  unit: name.endsWith('sufficiency') ? '%' : 'Wh',
  values: values.map(([period, v, missing = []]) => ({
    period,
    wh: v,
    complete: missing.length === 0,
    estimated: false,
    missing_days: missing.length,
    missing,
  })),
})

describe('history points', () => {
  it('follows the site series', () => {
    const full = site(['site/pv', 'site/grid_in', 'site/grid_out', 'site/consumption', 'site/self_sufficiency'])
    expect(historyKeys(full)).toEqual(['pv', 'consumption', 'gridIn', 'gridOut'])
    expect(historySeries(full)).toContain('site/self_sufficiency')
    expect(historyKeys(site(['site/consumption']))).toEqual(['consumption'])
    expect(historySeries(site(['site/consumption']))).toEqual([
      'site/consumption',
      'site/self_sufficiency',
    ])
  })

  it('splits the consumption only on a site with loadpoints', () => {
    const base = ['site/consumption', 'site/house_rest']
    expect(historyKeys(site(base))).toEqual(['consumption'])
    expect(historyKeys(site([...base, 'site/loadpoints/wallbox']))).toEqual([
      'consumption',
      'house',
      'wallbox',
    ])
    expect(chipKeys(['consumption', 'house', 'wallbox'])).toEqual(['house', 'wallbox'])
    expect(chipKeys(['consumption'])).toEqual(['consumption'])
  })

  it('matches values by period key and sums the range', () => {
    const r = latest('month', 3, '2026-11-19')
    const series = {
      'site/pv': energy('site/pv', [['2026-11', 3000], ['2026-09', 9000, ['2026-09-03', '2026-09-04']]]),
      'site/consumption': energy('site/consumption', [['2026-09', 10000], ['2026-11', 5000]]),
      'site/grid_in': energy('site/grid_in', [['2026-09', 4000], ['2026-11', 2500]]),
    }
    const pts = periodPoints(series, r)
    expect(pts.map((p) => p.start)).toEqual(['2026-09-01', '2026-10-01', '2026-11-01'])
    expect(pts[0]?.values.pv).toBe(9000)
    expect(pts[0]?.missing).toEqual(['2026-09-03', '2026-09-04'])
    expect(pts[2]?.missing).toEqual([])
    expect(pts[1]?.values.consumption).toBeNull()
    const keys = historyKeys(site(['site/pv', 'site/consumption', 'site/grid_in']))
    const sum = rangeSum(pts, keys)
    expect(sum.values).toMatchObject({ pv: 12000, consumption: 15000, gridIn: 6500, gridOut: null })
    expect(sum.selfSufficiency).toBeCloseTo((100 * 8500) / 15000)
    expect(sum).toMatchObject({ empty: 1, total: 3 })
  })

  it('has no self-sufficiency without consumption', () => {
    const pts = periodPoints({}, latest('day', 2, '2026-11-19'))
    expect(rangeSum(pts, ['consumption']).selfSufficiency).toBeNull()
  })
})
