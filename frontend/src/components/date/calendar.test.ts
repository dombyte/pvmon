import { describe, expect, it } from 'vitest'
import {
  clampDate,
  monthGrid,
  periodCells,
  periodOf,
  periodSelectable,
  selectable,
  shiftMonth,
  shiftPeriod,
} from './calendar'

describe('calendar', () => {
  it('builds six Monday-first weeks around the month', () => {
    const g = monthGrid('2026-11-19')
    expect(g).toHaveLength(42)
    expect(g[0]).toEqual({ date: '2026-10-26', inMonth: false }) // 1 Nov 2026 is a Sunday
    expect(g[6]).toEqual({ date: '2026-11-01', inMonth: true })
    expect(g.filter((c) => c.inMonth)).toHaveLength(30)
    expect(monthGrid('2026-06-10')[0]?.date).toBe('2026-06-01') // a month starting on Monday
  })

  it('moves months and checks bounds', () => {
    expect(shiftMonth('2026-01-31', -1)).toBe('2025-12-01')
    expect(shiftMonth('2026-11-19', 2)).toBe('2027-01-01')
    expect(selectable('2026-11-19', '2026-11-01', '2026-11-19')).toBe(true)
    expect(selectable('2026-11-20', undefined, '2026-11-19')).toBe(false)
    expect(selectable('2025-01-01', '2026-01-01')).toBe(false)
    expect(clampDate('2026-12-01', '2026-01-01', '2026-11-19')).toBe('2026-11-19')
    expect(clampDate('2025-12-01', '2026-01-01')).toBe('2026-01-01')
    expect(clampDate('2026-05-01')).toBe('2026-05-01')
  })
})

describe('month and year pickers', () => {
  it('lists the months of a year and a page of years', () => {
    const m = periodCells('month', '2026-11-19')
    expect(m).toHaveLength(12)
    expect(m[0]).toBe('2026-01-01')
    expect(m[11]).toBe('2026-12-01')
    const y = periodCells('year', '2026-11-19')
    expect(y[0]).toBe('2016-01-01')
    expect(y[11]).toBe('2027-01-01')
  })

  it('selects a period that overlaps the bounds', () => {
    expect(periodSelectable('month', '2026-11-01', '2026-11-15', '2026-11-19')).toBe(true)
    expect(periodSelectable('month', '2026-12-01', undefined, '2026-11-19')).toBe(false)
    expect(periodSelectable('year', '2024-01-01', '2024-06-03')).toBe(true)
    expect(periodSelectable('year', '2023-01-01', '2024-06-03')).toBe(false)
    expect(periodSelectable('day', '2026-11-20', undefined, '2026-11-19')).toBe(false)
  })

  it('shifts and normalises periods', () => {
    expect(periodOf('month', '2026-11-19')).toBe('2026-11-01')
    expect(periodOf('year', '2026-11-19')).toBe('2026-01-01')
    expect(periodOf('day', '2026-11-19')).toBe('2026-11-19')
    expect(shiftPeriod('month', '2026-11-01', 3)).toBe('2027-02-01')
    expect(shiftPeriod('year', '2026-01-01', -12)).toBe('2014-01-01')
  })
})
