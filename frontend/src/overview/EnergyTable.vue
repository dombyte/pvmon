<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Card from '@/components/Card.vue'
import EnergyCell from '@/energy/EnergyCell.vue'
import { missingDays, type EnergyRow } from '@/energy/rows'
import { formatDayList, type Lang } from '@/format'
import { columns, type Column } from '@/energy/useOverviewEnergy'
import type { EnergyValue } from '@/api/types'

const props = defineProps<{
  rows: EnergyRow[]
  phone: boolean
  value: (name: string, c: Column) => EnergyValue | undefined
}>()
const { t, locale } = useI18n()
const tab = ref<Column>('today')
const shown = computed(() => (props.phone ? [tab.value] : columns))
const heads = computed<Record<Column, string>>(() => ({
  today: t('energy.colToday'),
  month: t('energy.colMonth'),
  year: t('energy.colYear'),
  total: t('energy.colTotal'),
}))
// Spelled out below the table: the cell tooltip is out of reach on touch screens.
const missing = computed(() =>
  missingDays(props.rows.flatMap((r) => shown.value.map((c) => props.value(r.name, c)))),
)
const color = (r: EnergyRow) => (r.color === 'neutral' ? 'var(--ink-2)' : `var(--${r.color})`)
// Loadpoints are named by category only; how many there are is the flow's business.
const label = (r: EnergyRow) => t(r.labelKey)
</script>

<template>
  <Card :title="t('energy.title')">
    <div v-if="phone" class="mb-3 grid grid-cols-4 gap-1 rounded-control bg-well p-1" role="tablist">
      <button
        v-for="c in columns"
        :key="c"
        type="button"
        role="tab"
        :aria-selected="tab === c"
        class="min-h-11 rounded-chip text-sm"
        :class="tab === c ? 'toggle-on' : 'text-ink-2'"
        @click="tab = c"
      >
        {{ heads[c] }}
      </button>
    </div>
    <table class="w-full text-sm">
      <thead v-if="!phone">
        <tr class="border-b border-line text-ink-2">
          <th scope="col" class="py-2 text-left font-normal">{{ t('energy.colSeries') }}</th>
          <th v-for="c in shown" :key="c" scope="col" class="py-2 text-right font-normal">
            {{ heads[c] }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in rows" :key="r.name" class="border-b border-line last:border-0">
          <th scope="row" class="py-2.5 text-left font-normal">
            <span class="flex items-center gap-2">
              <span class="size-3.5 shrink-0 rounded-full border-2" :style="{ borderColor: color(r) }" />
              {{ label(r) }}
            </span>
          </th>
          <td v-for="c in shown" :key="c" class="py-2.5 text-right">
            <EnergyCell :value="value(r.name, c)" :ratio="r.ratio" :unit="phone" />
          </td>
        </tr>
      </tbody>
    </table>
    <p class="mt-2 text-xs text-ink-2">{{ t('energy.footnote') }}</p>
    <p v-if="missing.length" class="mt-1 text-xs text-ink-2">
      * {{ t('energy.incomplete', { days: formatDayList(missing, locale as Lang) }) }}
    </p>
  </Card>
</template>
