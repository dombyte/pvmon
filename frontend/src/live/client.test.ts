import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { LiveClient, type SocketLike } from './client'
import { useSubscription } from './useSubscription'

class FakeSocket implements SocketLike {
  static all: FakeSocket[] = []
  readyState = 0
  sent: { type: string; keys?: string[] }[] = []
  closed = false
  onopen: SocketLike['onopen'] = null
  onmessage: SocketLike['onmessage'] = null
  onclose: SocketLike['onclose'] = null
  onerror: SocketLike['onerror'] = null

  constructor(readonly url: string) {
    FakeSocket.all.push(this)
  }
  send(data: string): void {
    this.sent.push(JSON.parse(data))
  }
  close(): void {
    this.closed = true
    this.readyState = 3
    this.onclose?.({})
  }
  open(): void {
    this.readyState = 1
    this.onopen?.({})
  }
  push(frame: unknown): void {
    this.onmessage?.({ data: JSON.stringify(frame) })
  }
}

const last = (): FakeSocket => FakeSocket.all[FakeSocket.all.length - 1]!

function newClient(extra: Partial<ConstructorParameters<typeof LiveClient>[0]> = {}) {
  const c = new LiveClient({
    url: 'ws://x/ws',
    createSocket: (u) => new FakeSocket(u),
    ...extra,
  })
  c.start()
  return c
}

beforeEach(() => {
  FakeSocket.all = []
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

describe('LiveClient', () => {
  it('handles snapshot, update and removed', () => {
    const c = newClient()
    c.subscribe(['site/pv.power', 'battery/soc'])
    last().open()
    expect(last().sent).toEqual([{ type: 'subscribe', keys: ['site/pv.power', 'battery/soc'] }])

    last().push({
      type: 'snapshot',
      values: { 'site/pv.power': { value: 2410, unit: 'W', ts: 't0' }, 'battery/soc': { value: 80, unit: '%', ts: 't0' } },
    })
    expect(c.values['site/pv.power']).toEqual({ value: 2410, unit: 'W', ts: 't0' })

    last().push({ type: 'update', ts: 't1', values: { 'site/pv.power': { value: 2398 } }, removed: ['battery/soc'] })
    expect(c.values['site/pv.power']).toEqual({ value: 2398, unit: 'W', ts: 't1' })
    expect(c.values['battery/soc']).toBeUndefined()
    c.stop()
  })

  it('forwards device_state, status and site_changed', () => {
    const onDeviceState = vi.fn()
    const onStatus = vi.fn()
    const onSiteChanged = vi.fn()
    const c = newClient({ onDeviceState, onStatus, onSiteChanged })
    last().open()
    last().push({ type: 'device_state', device: 'meter', state: 'offline' })
    last().push({ type: 'status', device: 'wr1', register: 'operating', code: 4117, level: 'fault' })
    last().push({ type: 'site_changed', config_rev: 43 })
    expect(onDeviceState).toHaveBeenCalledWith('meter', 'offline')
    expect(onStatus).toHaveBeenCalledOnce()
    expect(onSiteChanged).toHaveBeenCalledWith(43)
    c.stop()
  })

  it('ignores frames that are not valid', () => {
    const c = newClient()
    c.subscribe(['a'])
    last().open()
    last().onmessage?.({ data: 'not json' })
    last().push({ nothing: true })
    expect(c.values).toEqual({})
    c.stop()
  })

  it('does not keep values of keys that are no longer subscribed', () => {
    const c = newClient()
    c.subscribe(['a'])
    last().open()
    c.unsubscribe(['a'])
    last().push({ type: 'update', ts: 't', values: { a: { value: 1 } } })
    expect(c.values.a).toBeUndefined()
    c.stop()
  })

  it('reference counts keys', () => {
    const c = newClient()
    last().open()
    c.subscribe(['a', 'b'])
    c.subscribe(['a'])
    expect(last().sent).toEqual([
      { type: 'subscribe', keys: ['a', 'b'] },
    ])
    c.unsubscribe(['a'])
    expect(last().sent).toHaveLength(1)
    c.unsubscribe(['a', 'b'])
    expect(last().sent[1]).toEqual({ type: 'unsubscribe', keys: ['a', 'b'] })
    c.stop()
  })

  it('reconnects with backoff and subscribes again', () => {
    const c = newClient()
    c.subscribe(['a', 'b'])
    last().open()
    last().close()
    expect(c.connected.value).toBe(false)
    expect(FakeSocket.all).toHaveLength(1)

    vi.advanceTimersByTime(1000)
    expect(FakeSocket.all).toHaveLength(2)
    last().open()
    expect(last().sent).toEqual([{ type: 'subscribe', keys: ['a', 'b'] }])

    // The delay was reset by the successful open; failed attempts then double it.
    last().close()
    vi.advanceTimersByTime(1000)
    expect(FakeSocket.all).toHaveLength(3)
    last().close() // never opened
    vi.advanceTimersByTime(1000)
    expect(FakeSocket.all).toHaveLength(3)
    vi.advanceTimersByTime(1000)
    expect(FakeSocket.all).toHaveLength(4)
    c.stop()
  })

  it('pings every 20 s and reconnects when no frame arrives within 5 s', () => {
    const c = newClient()
    c.subscribe(['a'])
    const first = last()
    first.open()
    vi.advanceTimersByTime(20_000)
    expect(first.sent.at(-1)).toEqual({ type: 'ping' })

    first.push({ type: 'pong' })
    vi.advanceTimersByTime(5_000)
    expect(first.closed).toBe(false)

    vi.advanceTimersByTime(15_000) // next ping
    vi.advanceTimersByTime(5_000) // nothing came back
    expect(first.closed).toBe(true)
    vi.advanceTimersByTime(1_000)
    expect(FakeSocket.all).toHaveLength(2)
    c.stop()
  })

  it('pings when the tab gets focus', () => {
    const c = newClient()
    last().open()
    window.dispatchEvent(new Event('focus'))
    expect(last().sent.at(-1)).toEqual({ type: 'ping' })
    c.stop()
  })

  it('reconnects at once when the tab gets focus while waiting in backoff', () => {
    const c = newClient()
    c.subscribe(['a'])
    for (let i = 0; i < 4; i++) {
      last().close() // never opened: the delay doubles to 16 s
      vi.advanceTimersByTime(30_000)
    }
    last().close()
    const before = FakeSocket.all.length
    window.dispatchEvent(new Event('focus'))
    expect(FakeSocket.all).toHaveLength(before + 1)
    last().open()
    expect(last().sent).toEqual([{ type: 'subscribe', keys: ['a'] }])
    vi.advanceTimersByTime(19_000) // past the 16 s retry, before the first ping timeout
    expect(FakeSocket.all).toHaveLength(before + 1) // the pending retry was cancelled
    c.stop()
  })

  it('splits more than 256 keys into several frames', () => {
    const c = newClient()
    last().open()
    c.subscribe(Array.from({ length: 300 }, (_, i) => `k${i}`))
    expect(last().sent.map((f) => f.keys?.length)).toEqual([256, 44])
    c.stop()
  })
})

describe('useSubscription', () => {
  it('subscribes for the scope and unsubscribes on dispose', () => {
    const c = newClient()
    last().open()
    const scope = effectScope()
    const other = effectScope()
    scope.run(() => useSubscription(['a'], c))
    other.run(() => useSubscription(['a'], c))
    expect(last().sent).toEqual([{ type: 'subscribe', keys: ['a'] }])
    scope.stop()
    expect(last().sent).toHaveLength(1)
    other.stop()
    expect(last().sent[1]).toEqual({ type: 'unsubscribe', keys: ['a'] })
    c.stop()
  })

  it('follows reactive keys and exposes values', async () => {
    const { ref, nextTick } = await import('vue')
    const c = newClient()
    last().open()
    const keys = ref(['a'])
    const scope = effectScope()
    const sub = scope.run(() => useSubscription(keys, c))!
    last().push({ type: 'snapshot', values: { a: { value: 1 } } })
    expect(sub.values.value.a?.value).toBe(1)
    keys.value = ['b']
    await nextTick()
    expect(last().sent.slice(-2)).toEqual([
      { type: 'subscribe', keys: ['b'] },
      { type: 'unsubscribe', keys: ['a'] },
    ])
    scope.stop()
    c.stop()
  })
})
