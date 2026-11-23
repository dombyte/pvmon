import { beforeEach, describe, expect, it } from 'vitest'
import { cachedHeader, noteResponse, offlineSince } from './offline'

const res = (status: number, at?: string) =>
  new Response('{}', { status, headers: at ? { [cachedHeader]: at } : {} })

describe('offline mark', () => {
  beforeEach(() => {
    offlineSince.value = null
  })

  it('keeps the oldest kept answer until a fresh one arrives', () => {
    noteResponse(res(200, '2026-11-20T12:30:00Z'))
    noteResponse(res(200, '2026-11-20T11:00:00Z'))
    noteResponse(res(200, '2026-11-20T13:00:00Z'))
    expect(offlineSince.value).toBe('2026-11-20T11:00:00Z')
    noteResponse(res(503))
    expect(offlineSince.value).toBe('2026-11-20T11:00:00Z')
    noteResponse(res(200))
    expect(offlineSince.value).toBeNull()
  })
})
