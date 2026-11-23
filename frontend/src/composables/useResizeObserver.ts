import { onScopeDispose, watch, type Ref } from 'vue'

/** Calls back whenever the observed element's content box changes size. */
export function useResizeObserver(
  target: Ref<Element | null | undefined>,
  callback: (entry: ResizeObserverEntry) => void,
): void {
  let observer: ResizeObserver | null = null
  const stop = watch(
    target,
    (el) => {
      observer?.disconnect()
      observer = null
      if (!el) return
      observer = new ResizeObserver((entries) => {
        const last = entries[entries.length - 1]
        if (last) callback(last)
      })
      observer.observe(el)
    },
    { immediate: true, flush: 'post' },
  )
  onScopeDispose(() => {
    stop()
    observer?.disconnect()
  })
}
