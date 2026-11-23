import { describe, expect, it } from 'vitest'
import { axisLabel, periodTitle, rangeLabel } from './labels'
import { latest } from './range'

describe('history labels', () => {
  it('names periods and ranges in the chosen language', () => {
    expect(axisLabel('day', '2026-11-18', 'de')).toBe('18')
    expect(axisLabel('month', '2026-11-01', 'en')).toBe('Nov')
    expect(axisLabel('year', '2026-01-01', 'de')).toBe('2026')
    expect(periodTitle('month', '2026-11-01', 'de')).toBe('November 2026')
    expect(periodTitle('year', '2026-01-01', 'en')).toBe('2026')
    expect(periodTitle('day', '2026-11-18', 'en')).toBe('18 Nov 2026')
    expect(rangeLabel(latest('day', 30, '2026-11-19'), 'de')).toBe('21. Okt. – 19. Nov. 2026')
    expect(rangeLabel(latest('day', 90, '2026-01-10'), 'en')).toBe('13 Oct 2025 – 10 Jan 2026')
    expect(rangeLabel(latest('month', 12, '2026-11-19'), 'en')).toBe('Dec 2025 – Nov 2026')
    expect(rangeLabel(latest('year', 5, '2026-11-19'), 'de')).toBe('2022 – 2026')
  })
})
