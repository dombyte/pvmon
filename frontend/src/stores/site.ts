import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { errorKey, fetchSite } from '@/api/client'
import type { SiteModel } from '@/api/types'

/** Retry delays after a failed load (the server may still be starting). */
const retryStartMs = 1_000
const retryMaxMs = 30_000

export const useSiteStore = defineStore('site', () => {
  const model = ref<SiteModel | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  let retryMs = retryStartMs
  let retryTimer: ReturnType<typeof setTimeout> | undefined

  const devicesTotal = computed(() => model.value?.devices.length ?? 0)
  const devicesOnline = computed(
    () => model.value?.devices.filter((d) => d.state === 'online').length ?? 0,
  )

  /** Loads the model; a failure retries with backoff until it succeeds. */
  async function load(): Promise<void> {
    clearTimeout(retryTimer)
    loading.value = true
    try {
      model.value = await fetchSite()
      error.value = null
      retryMs = retryStartMs
    } catch (err) {
      error.value = errorKey(err)
      retryTimer = setTimeout(() => void load(), retryMs)
      retryMs = Math.min(retryMs * 2, retryMaxMs)
    } finally {
      loading.value = false
    }
  }

  /** Live device_state frames update the state without reloading the model. */
  function setDeviceState(id: string, state: string): void {
    const d = model.value?.devices.find((x) => x.id === id)
    if (d) d.state = state as typeof d.state
  }

  return { model, loading, error, devicesTotal, devicesOnline, load, setDeviceState }
})
