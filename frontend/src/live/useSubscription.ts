import { computed, inject, onScopeDispose, toValue, watch, type InjectionKey, type MaybeRefOrGetter } from 'vue'
import type { LiveClient } from './client'
import type { LiveValue } from './protocol'

export const liveKey: InjectionKey<LiveClient> = Symbol('live')

/**
 * Subscribes to live keys for the lifetime of the calling scope. Keys may be reactive;
 * the connection reference-counts them across components.
 */
export function useSubscription(keys: MaybeRefOrGetter<readonly string[]>, client?: LiveClient) {
  const live = client ?? inject(liveKey)
  if (!live) throw new Error('useSubscription: no live client provided')

  let current: string[] = []
  watch(
    () => [...toValue(keys)],
    (next) => {
      const added = next.filter((k) => !current.includes(k))
      const dropped = current.filter((k) => !next.includes(k))
      live.subscribe(added)
      live.unsubscribe(dropped)
      current = next
    },
    { immediate: true },
  )
  onScopeDispose(() => {
    live.unsubscribe(current)
    current = []
  })

  const values = computed<Record<string, LiveValue | undefined>>(() => {
    const out: Record<string, LiveValue | undefined> = {}
    for (const k of toValue(keys)) out[k] = live.values[k]
    return out
  })
  return { values, connected: live.connected }
}
