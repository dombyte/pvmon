import { computed, type Ref } from 'vue'
import { fetchGauge } from '@/api/client'
import type { GaugeSeries, SiteModel } from '@/api/types'
import { usePolled } from '@/composables/usePolled'
import { refreshMs, useEnergy } from '@/energy/useEnergy'
import type { LocalDate } from '@/format/localdate'
import { dayQuality, dayRequest, daySlots, type DayRequest } from './model'

/** Consumed energy of the splittable loadpoint meters that count one. */
const meterSums = (site: SiteModel, req: DayRequest) =>
  req.meters
    .map((m) => `${m.id}/energy_in`)
    .filter((n) => site.meters.some((m) => m.series.includes(n)))

/**
 * Sums of the day card: energies, loadpoints (also per meter) and self-sufficiency (every
 * site has it).
 */
const daySums = (site: SiteModel, req: DayRequest) => [
  ...meterSums(site, req),
  ...[
    'site/pv',
    'site/consumption',
    ...req.categories.map((c) => `site/loadpoints/${c}`),
    'site/grid_in',
    'site/grid_out',
    'site/battery_in',
    'site/battery_out',
  ].filter((n) => site.site_series.includes(n)),
  'site/self_sufficiency',
]

/** Everything the day view shows for one local date (refreshed while it is today). */
export function useDay(site: Ref<SiteModel | null>, date: Ref<LocalDate>) {
  const req = computed(() => (site.value ? dayRequest(site.value) : null))
  const buckets = useEnergy(() =>
    req.value && date.value
      ? { series: req.value.energy, level: 'bucket', from: date.value, to: date.value }
      : null,
  )
  const sums = useEnergy(() =>
    site.value && req.value && date.value
      ? {
          series: daySums(site.value, req.value),
          level: 'day',
          from: date.value,
          to: date.value,
        }
      : null,
  )
  const gauge = usePolled(
    () =>
      req.value && req.value.gauge.length > 0 && date.value
        ? { series: req.value.gauge, from: date.value, to: date.value }
        : null,
    fetchGauge,
    refreshMs,
  )
  const gauges = computed<Record<string, GaugeSeries>>(() =>
    Object.fromEntries((gauge.data.value?.series ?? []).map((s) => [s.name, s])),
  )
  const slots = computed(() =>
    req.value ? daySlots(buckets.series.value, gauges.value, req.value) : [],
  )
  const quality = computed(() => dayQuality(slots.value))
  const error = computed(() => buckets.error.value ?? sums.error.value ?? gauge.error.value)
  return { req, slots, sums: sums.series, quality, error }
}
