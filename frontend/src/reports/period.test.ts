import { describe, expect, it } from 'vitest'
import { presetOf, presetRange, reportQuery } from './period'

describe('report periods', () => {
  const today = '2026-11-20'
  it('builds the presets', () => {
    expect(presetRange('last_month', today)).toEqual({
      from: '2026-10-01',
      to: '2026-10-31',
    })
    expect(presetRange('this_month', today)).toEqual({
      from: '2026-11-01',
      to: today,
    })
    expect(presetRange('last_year', today)).toEqual({
      from: '2025-01-01',
      to: '2025-12-31',
    })
    expect(presetRange('this_year', today)).toEqual({
      from: '2026-01-01',
      to: today,
    })
    expect(presetRange('last_30', today)).toEqual({
      from: '2026-10-21',
      to: '2026-11-19',
    })
    expect(presetRange('last_month', '2026-01-15')).toEqual({
      from: '2025-12-01',
      to: '2025-12-31',
    })
    expect(presetOf('2025-01-01', '2025-12-31', today)).toBe('last_year')
    expect(presetOf('2025-01-02', '2025-12-31', today)).toBeNull()
  })

  it('reads the route', () => {
    expect(reportQuery({ from: '2026-11-19', to: '2026-11-19', compare: 'none' }, today)).toEqual({
      from: '2026-11-19',
      to: '2026-11-19',
      compare: 'none',
    })
    expect(reportQuery({ from: '2026-11-19', to: '2026-12-01', compare: 'x' }, today)).toEqual({
      from: '2026-10-01',
      to: '2026-10-31',
      compare: 'last_year',
    })
    expect(reportQuery({ from: ['a'] }, today).from).toBe('2026-10-01')
  })
})
