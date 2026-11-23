import { describe, expect, it } from 'vitest'
import type { EnergySeries } from '@/api/types'
import { dayPoints, maxOf, rangeStart, share, sumOf, trendKeys, trendSeries } from './trend'
import { r1, r3 } from './fixtures'

const value = (period: string, wh: number | null) => ({
  period,
  wh,
  complete: wh !== null,
  estimated: false,
  missing_days: wh === null ? 1 : 0,
})

const series = (name: string, values: ReturnType<typeof value>[]): EnergySeries => ({
  name,
  unit: 'Wh',
  values,
})

describe('trend', () => {
  it('spans n days ending today across a month and DST change', () => {
    expect(rangeStart('2026-11-19', 14)).toBe('2026-11-06')
    expect(rangeStart('2026-10-30', 7)).toBe('2026-10-24')
  })

  it('asks for pv only when the site has it', () => {
    const noPV = { ...r1, site_series: r1.site_series.filter((s) => s !== 'site/pv') }
    expect(trendSeries(r1)).toContain('site/pv')
    expect(trendSeries(noPV)).not.toContain('site/pv')
    expect(trendKeys(r1)).toEqual(['pv', 'consumption'])
    expect(trendKeys(noPV)).toEqual(['consumption'])
  })

  it('matches values by period and keeps missing days as null', () => {
    const s = {
      'site/pv': series('site/pv', [value('2026-11-19', 500), value('2026-11-17', 7800)]),
      'site/consumption': series('site/consumption', [value('2026-11-18', null)]),
    }
    const pts = dayPoints(s, '2026-11-17', '2026-11-19')
    expect(pts.map((p) => p.date)).toEqual(['2026-11-17', '2026-11-18', '2026-11-19'])
    expect(pts.map((p) => p.pv)).toEqual([7800, null, 500])
    expect(pts.map((p) => p.consumption)).toEqual([null, null, null])
    expect(pts.map((p) => p.running)).toEqual([false, false, true])
    expect(pts[0]?.selfSufficiency).toBeNull()
  })

  it('scales and sums only known values', () => {
    const s = {
      'site/pv': series('site/pv', [value('2026-11-18', 2000), value('2026-11-19', null)]),
      'site/consumption': series('site/consumption', [value('2026-11-18', 8000)]),
    }
    const pts = dayPoints(s, '2026-11-18', '2026-11-19')
    expect(maxOf(pts, ['pv', 'consumption'])).toBe(8000)
    expect(maxOf(pts, ['pv'])).toBe(2000)
    expect(maxOf([], ['pv'])).toBe(0)
    expect(sumOf(pts, 'pv')).toBe(2000)
    expect(sumOf(pts.slice(1), 'pv')).toBeNull()
  })

  it('clamps bar shares', () => {
    expect(share(null, 10)).toBe(0)
    expect(share(5, 0)).toBe(0)
    expect(share(5, 10)).toBe(50)
    expect(share(20, 10)).toBe(100)
    expect(share(-1, 10)).toBe(0)
  })

  it('handles r3 as well', () => {
    expect(trendSeries(r3)).toContain('site/self_sufficiency')
  })
})
