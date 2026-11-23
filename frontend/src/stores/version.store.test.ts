import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const fetchVersion = vi.fn()
vi.mock('@/api/client', () => ({ fetchVersion: () => fetchVersion() }))

describe('version store', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubEnv('VITE_APP_VERSION', '1.0.0-abc')
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('shows the banner for a newer server until it is dismissed for that version', async () => {
    const { useVersionStore } = await import('./version')
    const v = useVersionStore()
    fetchVersion.mockResolvedValue({ version: '1.1.0-def' })
    expect(await v.checkNow()).toBe('available')
    expect(v.banner).toBe(true)
    v.dismiss()
    expect(v.banner).toBe(false)
    fetchVersion.mockResolvedValue({ version: '1.2.0-ghi' })
    await v.check()
    expect(v.banner).toBe(true) // a newer version shows it again
  })

  it('reports current, dev and failed checks', async () => {
    const { useVersionStore } = await import('./version')
    const v = useVersionStore()
    fetchVersion.mockResolvedValue({ version: '1.0.0-abc' })
    expect(await v.checkNow()).toBe('current')
    fetchVersion.mockResolvedValue({ version: 'dev' })
    expect(await v.checkNow()).toBe('dev')
    fetchVersion.mockRejectedValue(new Error('down'))
    expect(await v.checkNow()).toBe('failed')
    expect(v.checking).toBe(false)
  })
})
