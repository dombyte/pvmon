import { computed, type Ref } from 'vue'
import type { EnergyLevel, EnergyValue, SiteModel } from '@/api/types'
import { todayIn } from '@/format/localdate'
import { energyRows } from './rows'
import { useEnergy } from './useEnergy'

export type Column = 'today' | 'month' | 'year' | 'total'
export const columns: Column[] = ['today', 'month', 'year', 'total']
const levels: Record<Column, EnergyLevel> = {
  today: 'day',
  month: 'month',
  year: 'year',
  total: 'total',
}

/** Today, this month, this year and total for every overview row (four requests). */
export function useOverviewEnergy(site: Ref<SiteModel | null>) {
  const rows = computed(() => (site.value ? energyRows(site.value) : []))
  const today = computed(() => (site.value ? todayIn(site.value.tz) : ''))
  const query = (c: Column) => () =>
    site.value && rows.value.length
      ? { series: rows.value.map((r) => r.name), level: levels[c], from: today.value, to: today.value }
      : null
  const results = Object.fromEntries(columns.map((c) => [c, useEnergy(query(c))])) as Record<
    Column,
    ReturnType<typeof useEnergy>
  >
  function value(name: string, c: Column): EnergyValue | undefined {
    return results[c].series.value[name]?.values[0]
  }
  const error = computed(() => columns.map((c) => results[c].error.value).find((e) => e) ?? null)
  return { rows, value, error }
}
