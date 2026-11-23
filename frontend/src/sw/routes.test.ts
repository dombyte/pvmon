import { describe, expect, it } from 'vitest'
import { routeOf } from './routes'

const o = 'https://pv.local'
const r = (path: string, mode = 'cors', method = 'GET') => routeOf(new URL(path, o), method, mode, o)

describe('service worker routes', () => {
  it('serves pages from the accepted shell and built files from the cache', () => {
    expect(r('/day?date=2026-11-18', 'navigate')).toBe('shell')
    expect(r('/', 'navigate')).toBe('shell')
    expect(r('/assets/index-abc.js')).toBe('asset')
    expect(r('/icon-192.png', 'no-cors')).toBe('asset')
  })

  it('keeps API answers for offline use, never the version or downloads', () => {
    expect(r('/api/v1/site')).toBe('api')
    expect(r('/api/v1/energy?series=site/pv')).toBe('api')
    expect(r('/api/v1/version')).toBe('network')
    expect(r('/api/v1/report?from=a&to=b&format=pdf')).toBe('network')
    expect(r('/ws')).toBe('network')
    expect(r('/health')).toBe('network')
    expect(r('/docs/', 'navigate')).toBe('network')
  })

  it('leaves writes and other origins alone', () => {
    expect(r('/api/v1/config', 'cors', 'POST')).toBe('network')
    expect(routeOf(new URL('https://other.example/x.js'), 'GET', 'cors', o)).toBe('network')
  })
})

describe('offline mark', () => {
  it('is the header the app reads', async () => {
    const { cachedHeader } = await import('./routes')
    const app = await import('@/pwa/offline')
    expect(app.cachedHeader).toBe(cachedHeader)
  })
})
