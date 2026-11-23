import { describe, expect, it } from 'vitest'
import {
  formatDayList,
  formatDuration,
  formatDateTime,
  formatEnergy,
  formatLocalDate,
  formatNumber,
  formatPercent,
  formatPower,
  missing,
} from './index'

const nb = ' '

describe('formatPower', () => {
  it.each([
    [850, 'de', `850${nb}W`],
    [-850.4, 'en', `-850${nb}W`],
    [999.4, 'de', `999${nb}W`],
    [1000, 'de', `1,0${nb}kW`],
    [2410, 'de', `2,4${nb}kW`],
    [2410, 'en', `2.4${nb}kW`],
    [-12345, 'en', `-12.3${nb}kW`],
  ] as const)('%d W in %s', (w, lang, want) => {
    expect(formatPower(w, lang)).toBe(want)
  })
})

describe('formatEnergy', () => {
  it('uses the locale of the chosen language', () => {
    expect(formatEnergy(4_512_300, 'de')).toBe(`4.512,3${nb}kWh`)
    expect(formatEnergy(4_512_300, 'en')).toBe(`4,512.3${nb}kWh`)
  })
})

describe('formatPercent and formatNumber', () => {
  it('formats percent and plain numbers', () => {
    expect(formatPercent(87.4, 'de')).toBe(`87${nb}%`)
    expect(formatNumber(1234.567, 'de', 2)).toBe('1.234,57')
    expect(formatNumber(1234.567, 'en', 2)).toBe('1,234.57')
    expect(formatNumber(0.2, 'de', 1, 0)).toBe('0,2')
    expect(formatNumber(2, 'de', 1, 0)).toBe('2')
  })
  it('never turns missing data into zero', () => {
    expect(formatPower(null, 'en')).toBe(missing)
    expect(formatEnergy(undefined, 'de')).toBe(missing)
    expect(formatPercent(Number.NaN, 'de')).toBe(missing)
  })
})

describe('dates', () => {
  it('formats local dates without a timezone shift', () => {
    expect(formatLocalDate('2026-11-18', 'de', { day: '2-digit', month: '2-digit', year: 'numeric' })).toBe('18.11.2026')
    expect(formatLocalDate('2026-11-18', 'en', { day: '2-digit', month: '2-digit', year: 'numeric' })).toBe('18/11/2026')
    expect(formatLocalDate('2026-02-30', 'en')).toBe(missing)
  })
  it('formats instants in the site timezone', () => {
    const t = '2026-11-18T23:30:00Z'
    expect(formatDateTime(t, 'en', 'Europe/Berlin', { day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })).toBe('19, 00:30')
    expect(formatDateTime('nope', 'en', 'UTC')).toBe(missing)
  })
})

describe('negative zero', () => {
  it('never shows a sign on values that round to zero', () => {
    expect(formatNumber(-0.04, 'de', 1)).toBe('0,0')
    expect(formatNumber(-1.25, 'de', 1)).toBe('-1,3')
  })
})

describe('formatDuration', () => {
  it('shows hours and minutes', () => {
    expect(formatDuration(30_000, 'de')).toBe('< 1 min')
    expect(formatDuration(45 * 60_000, 'de')).toBe('45 min')
    expect(formatDuration(3 * 3_600_000 + 12 * 60_000, 'en')).toBe('3 h 12 min')
    expect(formatDuration(24 * 3_600_000, 'de')).toBe('24 h')
  })
})

describe('formatDayList', () => {
  it('lists dates of one year without the year', () => {
    expect(formatDayList(['2026-11-17', '2026-11-18'], 'en')).toBe('17 Nov, 18 Nov')
    expect(formatDayList([], 'en')).toBe('')
  })
  it('shows the newest days and counts the rest', () => {
    const days = ['2026-11-14', '2026-11-15', '2026-11-16', '2026-11-17']
    expect(formatDayList(days, 'en', 2)).toBe('16 Nov, 17 Nov (+2)')
  })
  it('shows the year when the dates span years', () => {
    expect(formatDayList(['2025-12-31', '2026-01-01'], 'de')).toBe('31.12.2025, 01.01.2026')
  })
})
