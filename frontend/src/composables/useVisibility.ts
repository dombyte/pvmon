import { onScopeDispose, ref, type Ref } from 'vue'

/** True while the tab is visible. */
export function useVisibility(): Ref<boolean> {
  const visible = ref(document.visibilityState === 'visible')
  const onChange = (): void => {
    visible.value = document.visibilityState === 'visible'
  }
  document.addEventListener('visibilitychange', onChange)
  onScopeDispose(() => document.removeEventListener('visibilitychange', onChange))
  return visible
}
