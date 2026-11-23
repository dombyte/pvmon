// Update check (spec 11.11): the UI compares the version it was built with against the
// running server. A different one means the server was updated and this page is old; the
// service worker caches nothing, so a reload always gets the new UI. No internet access.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { fetchVersion } from '@/api/client'
import { readStored, writeStored } from '@/composables/storage'
import { applyUpdate } from '@/pwa/update'

const pollMs = 5 * 60 * 1000
const devVersion = 'dev'
const dismissKey = 'pvmon.update.dismissed'

/** Version this bundle was built with (VITE_APP_VERSION, "dev" without one). */
export const builtVersion: string = import.meta.env.VITE_APP_VERSION || devVersion

/** True when a different, non-dev server version means the UI is outdated. */
export function isOutdated(built: string, server: string | null): boolean {
  if (server === null || built === devVersion || server === devVersion) return false
  return built !== server
}

/** Result of a manual check. */
export type CheckResult = 'current' | 'available' | 'dev' | 'failed'

export const useVersionStore = defineStore('version', () => {
  const serverVersion = ref<string | null>(null)
  const outdated = computed(() => isOutdated(builtVersion, serverVersion.value))
  /** Server version whose banner the user closed; a newer one shows it again. */
  const dismissed = ref(readStored(dismissKey))
  const banner = computed(() => outdated.value && dismissed.value !== serverVersion.value)
  const checking = ref(false)
  let timer: ReturnType<typeof setInterval> | undefined

  async function check(): Promise<boolean> {
    try {
      serverVersion.value = (await fetchVersion()).version
      return true
    } catch {
      // A failed check is retried at the next tick or focus; nothing to show.
      return false
    }
  }

  /** The "check for updates" button: asks the server now and says what it found. */
  async function checkNow(): Promise<CheckResult> {
    checking.value = true
    try {
      if (!(await check())) return 'failed'
      if (builtVersion === devVersion || serverVersion.value === devVersion) return 'dev'
      return outdated.value ? 'available' : 'current'
    } finally {
      checking.value = false
    }
  }

  /** Set when an accepted update could not be fetched yet (offline, server busy). */
  const unavailable = ref(false)
  /** Loads the new UI the user asked for (through the service worker when there is one). */
  async function update(): Promise<void> {
    unavailable.value = (await applyUpdate(builtVersion)) === 'unavailable'
  }

  function dismiss(): void {
    dismissed.value = serverVersion.value
    if (serverVersion.value) writeStored(dismissKey, serverVersion.value)
  }

  function onVisible(): void {
    if (document.visibilityState === 'visible') void check()
  }

  function start(): void {
    if (timer !== undefined) return
    void check()
    timer = setInterval(() => void check(), pollMs)
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('focus', onVisible)
  }

  function stop(): void {
    if (timer !== undefined) clearInterval(timer)
    timer = undefined
    document.removeEventListener('visibilitychange', onVisible)
    window.removeEventListener('focus', onVisible)
  }

  return {
    serverVersion,
    outdated,
    banner,
    checking,
    unavailable,
    check,
    checkNow,
    update,
    dismiss,
    start,
    stop,
  }
})
