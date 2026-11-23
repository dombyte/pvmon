import { computed, type Ref } from 'vue'
import type { SiteModel } from '@/api/types'
import { useEnergy } from '@/energy/useEnergy'
import { todayIn } from '@/format/localdate'
import { dayPoints, rangeStart, trendDays, trendKeys, trendSeries } from './trend'

/** The last 14 days of PV, consumption, grid and self-sufficiency (one request). */
export function useTrend(site: Ref<SiteModel | null>) {
  const today = computed(() => (site.value ? todayIn(site.value.tz) : ''))
  const from = computed(() => (today.value ? rangeStart(today.value, trendDays) : ''))
  const { series, error } = useEnergy(() =>
    site.value
      ? { series: trendSeries(site.value), level: 'day', from: from.value, to: today.value }
      : null,
  )
  const points = computed(() =>
    today.value ? dayPoints(series.value, from.value, today.value) : [],
  )
  const keys = computed(() => (site.value ? trendKeys(site.value) : []))
  return { points, keys, error }
}
