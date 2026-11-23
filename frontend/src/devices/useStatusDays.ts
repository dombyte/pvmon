import { computed, ref, watch } from 'vue'
import { readStored, writeStored } from '@/composables/storage'
import { todayIn } from '@/format/localdate'
import { useLayout } from '@/layout/mode'
import { useSiteStore } from '@/stores/site'
import { statusDays } from './status'

const storageKey = 'pvmon.status.days'

/** Day choices of the status bars on desktop. */
export const statusChoices = [30, 90] as const

/**
 * Days of the status bars: 30 or 90 on desktop (stored per browser, shared by the status
 * and device pages), always 30 in the compact layout.
 */
export function useStatusDays() {
  const site = useSiteStore()
  const { mode } = useLayout()
  const stored = ref(readStored(storageKey) === '90' ? 90 : 30)
  watch(stored, (n) => writeStored(storageKey, String(n)))
  const n = computed(() => (mode.value === 'compact' ? 30 : stored.value))
  const today = computed(() => (site.model ? todayIn(site.model.tz) : ''))
  const days = computed(() => (today.value ? statusDays(today.value, n.value) : []))
  return { stored, n, today, days, desktop: computed(() => mode.value === 'desktop') }
}
