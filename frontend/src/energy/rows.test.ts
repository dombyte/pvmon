import { describe, expect, it } from 'vitest'
import { gridOnly, pvGrid, r1, r4 } from '@/overview/fixtures'
import { energyRows, kpiRows, missingDays, numberOf } from './rows'

describe('energyRows', () => {
  it('shows battery and loadpoints only when the site has them', () => {
    const names = energyRows(r4).map((r) => r.name)
    expect(names).toEqual([
      'site/pv',
      'site/consumption',
      'site/grid_in',
      'site/grid_out',
      'site/battery_in',
      'site/battery_out',
      'site/house_rest',
      'site/loadpoints/wallbox',
      'site/self_sufficiency',
      'site/self_consumption',
    ])
    expect(energyRows(r4).find((r) => r.name === 'site/loadpoints/wallbox')?.labelKey).toBe('roles.wallbox')
    expect(energyRows(pvGrid).map((r) => r.name)).not.toContain('site/battery_in')
    expect(energyRows(pvGrid).map((r) => r.name)).not.toContain('site/house_rest')
    expect(energyRows(r1).map((r) => r.name)).toContain('site/battery_out')
  })

  it('has no PV rows without PV', () => {
    const names = energyRows(gridOnly).map((r) => r.name)
    expect(names).not.toContain('site/pv')
    expect(names).not.toContain('site/self_consumption')
    expect(names).toContain('site/self_sufficiency')
  })
})

describe('kpiRows', () => {
  it('desktop: six tiles in fixed order; phone: four without self-consumption', () => {
    const rows = energyRows(r4)
    expect(kpiRows(rows, false).map((r) => r.name)).toEqual([
      'site/pv',
      'site/consumption',
      'site/self_sufficiency',
      'site/self_consumption',
      'site/grid_in',
      'site/grid_out',
    ])
    expect(kpiRows(rows, true).map((r) => r.name)).toEqual([
      'site/pv',
      'site/consumption',
      'site/self_sufficiency',
      'site/grid_in',
    ])
  })
})

describe('numberOf', () => {
  const base = { period: 'p', complete: true, estimated: false, missing_days: 0, missing: [] }
  it('reads wh, pct or null — never 0 for missing', () => {
    expect(numberOf(undefined)).toBeNull()
    expect(numberOf({ ...base, wh: null })).toBeNull()
    expect(numberOf({ ...base, wh: 0 })).toBe(0)
    expect(numberOf({ ...base, pct: 58 })).toBe(58)
  })
})

describe('missingDays', () => {
  const v = (wh: number | null, missing: string[]) => ({
    period: 'p', wh, complete: !missing.length, estimated: false, missing_days: missing.length, missing,
  })
  it('joins the days of values with a number, sorted and unique', () => {
    const got = missingDays([v(1, ['2026-11-18']), v(2, ['2026-11-17', '2026-11-18']), undefined])
    expect(got).toEqual(['2026-11-17', '2026-11-18'])
  })
  it('skips values without a number: they are a gap already', () => {
    expect(missingDays([v(null, ['2026-11-18'])])).toEqual([])
  })
})
