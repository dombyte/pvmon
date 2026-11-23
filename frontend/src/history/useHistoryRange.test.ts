import { describe, expect, it } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { readPrefs, useHistoryRange } from './useHistoryRange'

describe('history preferences', () => {
  it('keeps only valid values', () => {
    expect(readPrefs(null)).toEqual({ gran: 'day', n: { day: 30, month: 12, year: 5 } })
    expect(readPrefs('{"gran":"month","n":{"day":7,"month":13,"year":10}}')).toEqual({
      gran: 'month',
      n: { day: 7, month: 12, year: 10 },
    })
    expect(readPrefs('{broken').gran).toBe('day')
    expect(readPrefs('{"gran":"week"}').gran).toBe('day')
  })

  it('switches, shifts and stores the range', async () => {
    localStorage.clear()
    const scope = effectScope()
    const today = ref('2026-11-19')
    const h = scope.run(() => useHistoryRange(today))!
    expect(h.range.value).toEqual({ gran: 'day', n: 30, end: '2026-11-19' })
    h.move(-1)
    expect(h.range.value?.end).toBe('2026-10-20')
    h.move(1)
    expect(h.range.value?.end).toBe('2026-11-19')
    h.setCustom({ gran: 'day', n: 10, end: '2026-11-05' })
    expect(h.range.value).toEqual({ gran: 'day', n: 10, end: '2026-11-05' })
    expect(h.custom.value).toBe(true)
    h.setCustom({ gran: 'day', n: 3, end: '2026-11-19' })
    expect(h.range.value).toEqual({ gran: 'day', n: 3, end: '2026-11-19' })
    h.setGranularity('year')
    expect(h.custom.value).toBe(false)
    h.setLength(10)
    expect(h.range.value).toEqual({ gran: 'year', n: 10, end: '2026-11-19' })
    await nextTick()
    expect(readPrefs(localStorage.getItem('pvmon.history')).n.year).toBe(10)
    today.value = ''
    expect(h.range.value).toBeNull()
    h.move(1)
    scope.stop()
  })
})
