import { describe, expect, it } from 'vitest'
import {
  addDays,
  addMonths,
  addYears,
  daysInMonth,
  diffDays,
  parseLocalDate,
  startOfMonth,
  todayIn,
} from './localdate'

describe('local dates', () => {
  it('parses only real calendar dates', () => {
    expect(parseLocalDate('2026-11-19')).toEqual({ y: 2026, m: 11, d: 19 })
    expect(parseLocalDate('2026-02-29')).toBeNull()
    expect(parseLocalDate('19.11.2026')).toBeNull()
  })
  it('adds days across month, year and leap boundaries', () => {
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addDays('2028-03-01', -1)).toBe('2028-02-29')
  })
  it('is not shifted by DST changes', () => {
    expect(addDays('2026-03-28', 2)).toBe('2026-03-30')
    expect(diffDays('2026-10-24', '2026-10-26')).toBe(2)
  })
  it('clamps months and years', () => {
    expect(addMonths('2026-01-31', 1)).toBe('2026-02-28')
    expect(addMonths('2026-01-15', -2)).toBe('2025-11-15')
    expect(addYears('2028-02-29', 1)).toBe('2029-02-28')
  })
  it('knows month lengths and starts', () => {
    expect(daysInMonth(2028, 2)).toBe(29)
    expect(startOfMonth('2026-11-19')).toBe('2026-11-01')
  })
  it('computes today in the site timezone', () => {
    const now = new Date('2026-11-18T23:30:00Z')
    expect(todayIn('Europe/Berlin', now)).toBe('2026-11-19')
    expect(todayIn('UTC', now)).toBe('2026-11-18')
  })
})
