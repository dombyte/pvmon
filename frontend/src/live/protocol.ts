// WebSocket protocol of spec 9.3.

export interface LiveValue {
  value: number | string
  unit?: string
  ts?: string
  /** An estimate, e.g. SoC averaged without capacities. */
  approx?: boolean
}

export type ServerFrame =
  | { type: 'snapshot'; values: Record<string, LiveValue> }
  | { type: 'update'; ts?: string; values?: Record<string, LiveValue>; removed?: string[] }
  | { type: 'device_state'; device: string; state: string }
  | { type: 'site_changed'; config_rev: number }
  | { type: 'status'; device: string; register: string; code: number; level: string }
  | { type: 'error'; code: string; keys?: string[] }
  | { type: 'pong' }

export type ClientFrame =
  | { type: 'subscribe'; keys: string[] }
  | { type: 'unsubscribe'; keys: string[] }
  | { type: 'ping' }

/** Keys per subscribe/unsubscribe frame the server accepts. */
export const maxKeysPerFrame = 256

/** Parses a text frame; anything that is not a known frame is dropped (null). */
export function parseFrame(data: unknown): ServerFrame | null {
  if (typeof data !== 'string') return null
  try {
    const v: unknown = JSON.parse(data)
    if (typeof v === 'object' && v !== null && typeof (v as { type?: unknown }).type === 'string') {
      return v as ServerFrame
    }
  } catch {
    // fall through
  }
  return null
}
