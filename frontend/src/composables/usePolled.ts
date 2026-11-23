import { onScopeDispose, ref, shallowRef, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { errorKey } from '@/api/client'
import { useVisibility } from './useVisibility'

/**
 * Loads `fetcher(query)` for a reactive query (null = nothing to load) and reloads it
 * every `everyMs` while the tab is visible (0 = only on query change and tab focus). The
 * last answer stays until the next one, so pages do not flicker; a newer query aborts an
 * older request.
 */
export function usePolled<Q, T>(
  query: MaybeRefOrGetter<Q | null>,
  fetcher: (q: Q, signal: AbortSignal) => Promise<T>,
  everyMs: number,
) {
  const data = shallowRef<T | null>(null)
  const error = ref<string | null>(null)
  const visible = useVisibility()
  let ctrl: AbortController | null = null

  async function load(): Promise<void> {
    const q = toValue(query)
    if (q === null) return
    ctrl?.abort()
    const mine = new AbortController()
    ctrl = mine
    try {
      data.value = await fetcher(q, mine.signal)
      error.value = null
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      error.value = errorKey(err)
    }
  }

  watch(() => JSON.stringify(toValue(query)), () => void load(), { immediate: true })
  watch(visible, (v) => {
    if (v) void load()
  })
  const timer = everyMs > 0 ? setInterval(() => visible.value && void load(), everyMs) : undefined
  onScopeDispose(() => {
    clearInterval(timer)
    ctrl?.abort()
  })
  return { data, error, reload: load }
}
