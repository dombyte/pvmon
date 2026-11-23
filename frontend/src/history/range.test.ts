import { describe, expect, it } from 'vitest'
import { atToday, bounds, customRange, latest, periodLastDay, periods, shift } from './range'

describe('history range', () => {
  const today = '2026-11-19'

  it('covers n days ending today', () => {
    const r = latest('day', 30, today)
    expect(bounds(r)).toEqual({ from: '2026-10-21', to: today })
    expect(periods(r)).toHaveLength(30)
    expect(atToday(r, today)).toBe(true)
  })

  it('covers whole months and years', () => {
    expect(bounds(latest('month', 12, today))).toEqual({ from: '2025-12-01', to: today })
    expect(periods(latest('month', 3, today))).toEqual(['2026-09-01', '2026-10-01', '2026-11-01'])
    expect(bounds(latest('year', 5, today))).toEqual({ from: '2022-01-01', to: today })
  })

  it('shifts by its length and never past today', () => {
    const back = shift(latest('day', 7, today), -1, today)
    expect(bounds(back)).toEqual({ from: '2026-11-06', to: '2026-11-12' })
    expect(atToday(back, today)).toBe(false)
    expect(shift(back, 1, today).end).toBe(today)
    const months = shift(latest('month', 6, today), -1, today)
    expect(bounds(months)).toEqual({ from: '2025-12-01', to: '2026-05-31' })
    expect(shift(latest('year', 3, today), -1, today).end).toBe('2023-12-31')
    expect(shift(latest('year', 3, today), 1, today).end).toBe(today)
  })

  it('ends the current period today', () => {
    expect(periodLastDay('month', '2026-11-01', today)).toBe(today)
    expect(periodLastDay('month', '2024-02-01', today)).toBe('2024-02-29')
    expect(periodLastDay('year', '2025-01-01', today)).toBe('2025-12-31')
    expect(periodLastDay('day', '2026-11-18', today)).toBe('2026-11-18')
  })

  it('builds custom ranges from two dates in any order', () => {
    expect(customRange('day', '2026-11-01', '2026-11-10', today)).toEqual({ gran: 'day', n: 10, end: '2026-11-10' })
    expect(customRange('day', '2026-11-10', '2026-11-01', today).n).toBe(10)
    expect(customRange('month', '2026-03-15', '2026-11-19', today)).toEqual({ gran: 'month', n: 9, end: '2026-11-19' })
    expect(customRange('month', '2025-01-20', '2025-06-02', today)).toEqual({ gran: 'month', n: 6, end: '2025-06-30' })
    expect(customRange('year', '2020-05-01', '2026-01-01', today)).toEqual({ gran: 'year', n: 7, end: '2026-11-19' })
    expect(bounds(customRange('month', '2025-01-20', '2025-06-02', today)).from).toBe('2025-01-01')
  })
})
