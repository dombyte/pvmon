import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useSiteStore } from './site'

const model = { name: 'Home', tz: 'Europe/Berlin', devices: [], meters: [], site_series: [] }

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('site store', () => {
  it('retries a failed load with backoff until it succeeds', async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockResolvedValue(new Response(JSON.stringify(model), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    const site = useSiteStore()
    await site.load()
    expect(site.error).toBe('errors.network')
    await vi.advanceTimersByTimeAsync(1_000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1_999)
    expect(fetchMock).toHaveBeenCalledTimes(2) // the delay doubled
    await vi.advanceTimersByTimeAsync(1)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(site.model?.name).toBe('Home')
    expect(site.error).toBeNull()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(fetchMock).toHaveBeenCalledTimes(3) // no retry after success
  })
})
