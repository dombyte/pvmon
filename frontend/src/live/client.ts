import { reactive, ref } from 'vue'
import {
  maxKeysPerFrame,
  parseFrame,
  type ClientFrame,
  type LiveValue,
  type ServerFrame,
} from './protocol'

/** The part of WebSocket the client uses; tests supply a fake. */
export interface SocketLike {
  readyState: number
  onopen: ((ev: unknown) => void) | null
  onmessage: ((ev: { data: unknown }) => void) | null
  onclose: ((ev: unknown) => void) | null
  onerror: ((ev: unknown) => void) | null
  send(data: string): void
  close(): void
}

interface LiveHandlers {
  onDeviceState?: (device: string, state: string) => void
  onStatus?: (frame: Extract<ServerFrame, { type: 'status' }>) => void
  onSiteChanged?: (configRev: number) => void
}

export interface LiveOptions extends LiveHandlers {
  url: string
  createSocket?: (url: string) => SocketLike
}

const OPEN = 1
const pingIntervalMs = 20_000
const frameTimeoutMs = 5_000
const backoffStartMs = 1_000
const backoffMaxMs = 30_000

function chunk<T>(items: T[], size: number): T[][] {
  const out: T[][] = []
  for (let i = 0; i < items.length; i += size) out.push(items.slice(i, i + size))
  return out
}

/**
 * One WebSocket connection per app. Keys are reference counted: the first subscriber of a
 * key subscribes on the wire, the last one unsubscribes. After a reconnect every key still
 * in use is subscribed again.
 */
export class LiveClient {
  /** Newest value per subscribed key. */
  readonly values = reactive<Record<string, LiveValue>>({})
  readonly connected = ref(false)

  private readonly opts: LiveOptions
  private readonly counts = new Map<string, number>()
  private socket: SocketLike | null = null
  private running = false
  private backoff = backoffStartMs
  private pingTimer: ReturnType<typeof setInterval> | undefined
  private frameTimer: ReturnType<typeof setTimeout> | undefined
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined
  /** Back in view: check a live connection, or reconnect now instead of after backoff. */
  private readonly onFocus = (): void => {
    if (document.visibilityState !== 'visible') return
    if (this.socket) {
      this.ping()
      return
    }
    if (!this.running) return
    clearTimeout(this.reconnectTimer)
    this.backoff = backoffStartMs
    this.connect()
  }

  constructor(opts: LiveOptions) {
    this.opts = opts
  }

  start(): void {
    if (this.running) return
    this.running = true
    document.addEventListener('visibilitychange', this.onFocus)
    window.addEventListener('focus', this.onFocus)
    this.connect()
  }

  stop(): void {
    this.running = false
    document.removeEventListener('visibilitychange', this.onFocus)
    window.removeEventListener('focus', this.onFocus)
    this.clearTimers()
    const s = this.socket
    this.socket = null
    if (s) {
      s.onclose = null
      s.close()
    }
    this.connected.value = false
  }

  subscribe(keys: readonly string[]): void {
    const fresh: string[] = []
    for (const k of keys) {
      const n = this.counts.get(k) ?? 0
      this.counts.set(k, n + 1)
      if (n === 0) fresh.push(k)
    }
    this.sendKeys('subscribe', fresh)
  }

  unsubscribe(keys: readonly string[]): void {
    const gone: string[] = []
    for (const k of keys) {
      const n = this.counts.get(k) ?? 0
      if (n <= 1) {
        if (n === 1) gone.push(k)
        this.counts.delete(k)
        delete this.values[k]
      } else {
        this.counts.set(k, n - 1)
      }
    }
    this.sendKeys('unsubscribe', gone)
  }

  private sendKeys(type: 'subscribe' | 'unsubscribe', keys: string[]): void {
    for (const part of chunk(keys, maxKeysPerFrame)) this.send({ type, keys: part })
  }

  private send(frame: ClientFrame): void {
    const s = this.socket
    if (s && s.readyState === OPEN) s.send(JSON.stringify(frame))
  }

  private connect(): void {
    if (!this.running) return
    const create = this.opts.createSocket ?? ((u: string) => new WebSocket(u) as SocketLike)
    const s = create(this.opts.url)
    this.socket = s
    s.onopen = () => {
      this.backoff = backoffStartMs
      this.connected.value = true
      this.sendKeys('subscribe', [...this.counts.keys()])
      this.pingTimer = setInterval(() => this.ping(), pingIntervalMs)
    }
    s.onmessage = (ev) => {
      this.clearFrameTimer()
      const frame = parseFrame(ev.data)
      if (frame) this.handle(frame)
    }
    s.onclose = () => this.onClosed(s)
    s.onerror = () => undefined // onclose follows and drives the reconnect
  }

  private onClosed(s: SocketLike): void {
    if (this.socket !== s) return
    this.socket = null
    this.connected.value = false
    this.clearTimers()
    if (!this.running) return
    this.reconnectTimer = setTimeout(() => this.connect(), this.backoff)
    this.backoff = Math.min(this.backoff * 2, backoffMaxMs)
  }

  /** Pings and expects any frame within 5 s, otherwise drops the connection. */
  private ping(): void {
    const s = this.socket
    if (!s || s.readyState !== OPEN) return
    this.send({ type: 'ping' })
    this.clearFrameTimer()
    this.frameTimer = setTimeout(() => {
      // A silent connection is dead: close it and let onclose reconnect.
      s.close()
      this.onClosed(s)
    }, frameTimeoutMs)
  }

  private handle(frame: ServerFrame): void {
    switch (frame.type) {
      case 'snapshot':
        for (const [k, v] of Object.entries(frame.values)) this.values[k] = v
        break
      case 'update':
        this.applyUpdate(frame)
        break
      case 'device_state':
        this.opts.onDeviceState?.(frame.device, frame.state)
        break
      case 'status':
        this.opts.onStatus?.(frame)
        break
      case 'site_changed':
        this.opts.onSiteChanged?.(frame.config_rev)
        break
      default:
        break // pong and error frames only prove liveness
    }
  }

  private applyUpdate(frame: Extract<ServerFrame, { type: 'update' }>): void {
    for (const [k, v] of Object.entries(frame.values ?? {})) {
      // Only subscribed keys are kept; a late frame must not resurrect an unsubscribed key.
      if (!this.counts.has(k)) continue
      const prev = this.values[k]
      this.values[k] = { ...prev, ...v, ts: frame.ts ?? prev?.ts }
    }
    for (const k of frame.removed ?? []) delete this.values[k]
  }

  private clearFrameTimer(): void {
    if (this.frameTimer !== undefined) clearTimeout(this.frameTimer)
    this.frameTimer = undefined
  }

  private clearTimers(): void {
    this.clearFrameTimer()
    if (this.pingTimer !== undefined) clearInterval(this.pingTimer)
    if (this.reconnectTimer !== undefined) clearTimeout(this.reconnectTimer)
    this.pingTimer = undefined
    this.reconnectTimer = undefined
  }
}

/** ws(s) URL of the server the page came from. */
export function liveURL(loc: Pick<Location, 'protocol' | 'host'>): string {
  return `${loc.protocol === 'https:' ? 'wss' : 'ws'}://${loc.host}/ws`
}
