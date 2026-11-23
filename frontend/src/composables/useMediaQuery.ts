import { onScopeDispose, ref, type Ref } from 'vue'

/** Reactive `matchMedia`. */
export function useMediaQuery(query: string): Ref<boolean> {
  const mql = window.matchMedia(query)
  const matches = ref(mql.matches)
  const onChange = (ev: MediaQueryListEvent): void => {
    matches.value = ev.matches
  }
  mql.addEventListener('change', onChange)
  onScopeDispose(() => mql.removeEventListener('change', onChange))
  return matches
}
