import { useRouter } from 'vue-router'
import { useSessionStore } from '@/stores/session'

/** Logs out and leaves pages that need a login (admin pages, or all without public_read). */
export function useLogout() {
  const router = useRouter()
  const session = useSessionStore()
  return async (): Promise<void> => {
    try {
      await session.logout()
    } catch {
      // The session may already be gone; reload the state either way.
      await session.load().catch(() => undefined)
    }
    const admin = router.currentRoute.value.meta.admin === true
    if (admin || !session.canRead) await router.replace(session.canRead ? '/' : '/login')
  }
}
