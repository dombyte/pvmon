import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { fetchEnergy, type EnergyQuery } from '@/api/client'
import type { EnergySeries } from '@/api/types'
import { usePolled } from '@/composables/usePolled'

/** Today's values change with every bucket flush (60 s); refresh at that pace. */
export const refreshMs = 60_000

/** Loads energy for a reactive query (null = nothing to load), refreshed while visible. */
export function useEnergy(query: MaybeRefOrGetter<EnergyQuery | null>) {
  const { data, error, reload } = usePolled(
    () => {
      const q = toValue(query)
      return q && q.series.length > 0 ? q : null
    },
    fetchEnergy,
    refreshMs,
  )
  const series = computed<Record<string, EnergySeries>>(() =>
    Object.fromEntries((data.value?.series ?? []).map((s) => [s.name, s])),
  )
  return { series, error, reload }
}
