import { describe, expect, it } from 'vitest'
import type { StatusEvent, StatusLevel } from '@/api/types'
import { dayLevels, daySpans, levelColor, newest, registerDays, statusDays, worst } from './status'

const h = 3_600_000

const ev = (register: string, at: string, level: StatusLevel): StatusEvent => ({
  register,
  at: `${at}+01:00`,
  code: 0,
  level,
  texts: null,
})

describe('status history', () => {
  it('lists the days ending today', () => {
    expect(statusDays('2026-11-19', 3)).toEqual(['2026-11-17', '2026-11-18', '2026-11-19'])
  })

  it('keeps the worst level of each day and carries the state over', () => {
    const days = statusDays('2026-11-19', 4)
    const events = [
      ev('op', '2026-11-18T10:00:00', 'fault'),
      ev('op', '2026-11-15T08:00:00', 'ok'), // active at the start of the range
      ev('op', '2026-11-18T11:00:00', 'ok'),
      ev('late', '2026-11-18T09:00:00', 'notice'),
    ]
    expect(registerDays(events, ['op', 'late', 'gone'], days)).toEqual({
      op: ['ok', 'ok', 'fault', 'ok'],
      late: [null, null, 'notice', 'notice'],
      gone: [null, null, null, null],
    })
  })

  it('ranks offline above a notice and "none" below everything seen', () => {
    expect(worst('fault', 'ok')).toBe('fault')
    expect(worst('ok', 'notice')).toBe('notice')
    expect(worst('notice', 'offline')).toBe('offline')
    expect(worst('offline', 'fault')).toBe('fault')
    expect(worst('none', 'ok')).toBe('ok')
    const days = statusDays('2026-11-19', 3)
    const changes = [
      { at: '2026-11-17T08:00:00+01:00', level: 'none' as const },
      { at: '2026-11-18T08:00:00+01:00', level: 'ok' as const },
      { at: '2026-11-18T20:00:00+01:00', level: 'offline' as const },
    ]
    expect(dayLevels(changes, days)).toEqual(['none', 'offline', 'offline'])
  })

  it('finds the newest change and colours every level', () => {
    const cur = newest([ev('op', '2026-11-18T10:00:00', 'fault'), ev('op', '2026-11-18T11:00:00', 'ok')])
    expect(cur?.level).toBe('ok')
    expect(newest([])).toBeNull()
    expect(levelColor('offline')).toBe('var(--ink-2)')
    expect(levelColor(null)).toBe('var(--line)')
    expect(levelColor('none')).toBe('var(--line)')
  })
})

describe('day spans', () => {
  const tz = 'Europe/Berlin'
  const ch = (at: string, level: StatusLevel, key: string = level) => ({ at, level, key })

  it('sums how long each state lasted per day, today up to now', () => {
    const changes = [
      ch('2026-11-04T20:00:00+01:00', 'ok'),
      ch('2026-11-05T13:00:00+01:00', 'fault'),
      ch('2026-11-05T15:30:00+01:00', 'ok'),
    ]
    const now = Date.parse('2026-11-06T06:00:00+01:00')
    const [d4, d5, d6, d7] = daySpans(changes, ['2026-11-04', '2026-11-05', '2026-11-06', '2026-11-07'], tz, now)
    expect(d4).toEqual([{ key: 'ok', level: 'ok', ms: 4 * h }])
    expect(d5).toEqual([
      { key: 'ok', level: 'ok', ms: 21.5 * h },
      { key: 'fault', level: 'fault', ms: 2.5 * h },
    ])
    expect(d6).toEqual([{ key: 'ok', level: 'ok', ms: 6 * h }])
    expect(d7).toEqual([])
    expect(daySpans([], ['2026-11-04'], tz, now)).toEqual([[]])
  })

  it('keys register messages and knows 23-hour DST days', () => {
    const changes = [ch('2026-03-28T12:00:00+01:00', 'ok', 'Normal')]
    const [d] = daySpans(changes, ['2026-03-29'], tz, Date.parse('2026-04-01T00:00:00Z'))
    expect(d).toEqual([{ key: 'Normal', level: 'ok', ms: 23 * h }])
  })
})
