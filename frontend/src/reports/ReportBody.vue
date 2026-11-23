<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown } from '@lucide/vue'
import type { ReportBucket, ReportResponse } from '@/api/types'
import Card from '@/components/Card.vue'
import { formatEnergy, formatLocalDate, formatNumber, formatPercent, type Lang } from '@/format'
import DataTable from './DataTable.vue'
import FigureTiles from './FigureTiles.vue'
import { categoryLabel } from './labels'
import SplitBar, { type SplitPart } from './SplitBar.vue'

const props = defineProps<{ report: ReportResponse }>()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const kwhToWh = 1000
const minBalanceDays = 28
const r = computed(() => props.report)
/** The period table is a detail: folded until asked for. */
const showTable = ref(false)
const s = computed(() => r.value.summary)
const kwh = (v: number | null | undefined) =>
  formatEnergy(v == null ? null : v * kwhToWh, lang.value)
const pct = (v: number | null | undefined) => formatPercent(v, lang.value, 1)
const date = (d: string) => formatLocalDate(d, lang.value)
const hasPV = computed(() => s.value.total.pv_kwh != null)
const hasBattery = computed(() => s.value.total.charge_kwh != null)

const sources = computed<SplitPart[]>(() => {
  const x = s.value.sources
  const out: SplitPart[] = [
    {
      label: t('reports.solar'),
      color: 'var(--pv)',
      kwh: x.solar_est_kwh,
      pct: x.solar_pct,
    },
  ]
  if (hasBattery.value)
    out.push({
      label: t('roles.battery'),
      color: 'var(--battery)',
      kwh: x.battery_est_kwh,
      pct: x.battery_pct,
    })
  out.push({
    label: t('roles.grid'),
    color: 'var(--grid)',
    kwh: x.grid_est_kwh,
    pct: x.grid_pct,
  })
  return out
})
const pvSplit = computed<SplitPart[]>(() => {
  const x = s.value.pv_split
  if (!x) return []
  const out: SplitPart[] = [
    {
      label: t('reports.direct'),
      color: 'var(--home)',
      kwh: x.solar_est_kwh,
      pct: x.solar_pct,
    },
  ]
  if (hasBattery.value)
    out.push({
      label: t('reports.charged'),
      color: 'var(--battery)',
      kwh: x.battery_est_kwh,
      pct: x.battery_pct,
    })
  out.push({
    label: t('reports.exported'),
    color: 'var(--grid)',
    kwh: x.grid_est_kwh,
    pct: x.grid_pct,
  })
  return out
})
const ratio = (v: number | null | undefined) =>
  v == null ? t('reports.fromDays', { n: minBalanceDays }) : formatNumber(v, lang.value, 2, 2)
const battery = computed(() => [
  [t('reports.fig.charge'), kwh(s.value.total.charge_kwh)],
  [t('reports.fig.discharge'), kwh(s.value.total.discharge_kwh)],
  [`${t('reports.gridCharge')} ≈`, kwh(s.value.grid_charge_est_kwh)],
  [t('reports.ratio'), ratio(s.value.battery_ratio)],
  [
    t('reports.balanceDiff'),
    s.value.balance_diff_kwh == null
      ? t('reports.fromDays', { n: minBalanceDays })
      : `${kwh(s.value.balance_diff_kwh)} (${pct(s.value.balance_diff_pct)})`,
  ],
])
const lpRows = computed(() => {
  const lp = r.value.loadpoints
  if (!lp) return []
  const rows = lp.meters.map((m) => [
    m.label,
    categoryLabel(t, m.category),
    kwh(m.in_kwh),
    kwh(m.solar_est_kwh),
    pct(m.solar_pct),
  ])
  if (lp.categories && lp.categories.length < lp.meters.length)
    for (const c of lp.categories)
      rows.push([
        t('reports.sum'),
        categoryLabel(t, c.category),
        kwh(c.in_kwh),
        kwh(c.solar_est_kwh),
        pct(c.solar_pct),
      ])
  return rows
})
const patterns = computed(() => {
  const p = r.value.patterns
  const day = (d: { date: string; kwh: number } | null) =>
    d ? `${kwh(d.kwh)} · ${date(d.date)}` : '–'
  const rows = [
    [t('reports.maxConsumption'), day(p.max_consumption)],
    [t('reports.maxImport'), day(p.max_import)],
    [t('reports.weekday'), kwh(p.weekday_consumption_kwh)],
    [t('reports.weekend'), kwh(p.weekend_consumption_kwh)],
  ]
  if (hasPV.value)
    rows.unshift([t('reports.bestPV'), day(p.best_pv)], [t('reports.lowestPV'), day(p.lowest_pv)])
  return rows
})
const bucketLabel = (b: ReportBucket) => {
  if (r.value.granularity === 'year') return b.key
  if (r.value.granularity === 'month')
    return formatLocalDate(b.from, lang.value, {
      month: 'short',
      year: 'numeric',
    })
  return formatLocalDate(b.from, lang.value, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
  })
}
const tableHeader = computed(() => [
  t('reports.period'),
  ...(hasPV.value ? [t('reports.fig.pv')] : []),
  t('reports.fig.consumption'),
  t('reports.fig.gridImport'),
  t('reports.fig.gridExport'),
  ...(hasBattery.value ? [t('reports.fig.charge'), t('reports.fig.discharge')] : []),
  t('reports.fig.selfSufficiency'),
])
const tableRows = computed(() =>
  r.value.buckets.map((b) => {
    const none = b.complete_days === 0
    const v = (x: number | null) => (none ? '–' : kwh(x))
    const label =
      b.complete_days < b.days && r.value.granularity !== 'day'
        ? `${bucketLabel(b)} (${b.complete_days}/${b.days})`
        : bucketLabel(b)
    return [
      label,
      ...(hasPV.value ? [v(b.total.pv_kwh)] : []),
      v(b.total.consumption_kwh),
      v(b.total.import_kwh),
      v(b.total.export_kwh),
      ...(hasBattery.value ? [v(b.total.charge_kwh), v(b.total.discharge_kwh)] : []),
      none ? '–' : pct(b.self_sufficiency_pct),
    ]
  }),
)
const compareNote = computed(() => {
  const c = r.value.comparison
  if (!c) return ''
  const range = `${date(c.from)} – ${date(c.to)}`
  return c.matched
    ? t('reports.matched', { range, n: c.matched_days })
    : t('reports.comparedWith', { range })
})
</script>

<template>
  <Card :title="t('reports.figures')">
    <FigureTiles :figures="r.figures" :compared="r.comparison !== null" />
    <p v-if="compareNote" class="mt-3 text-sm text-ink-2">{{ compareNote }}</p>
  </Card>
  <Card :title="t('reports.flows')">
    <div class="flex flex-col gap-5">
      <SplitBar :title="t('reports.sources')" :parts="sources" />
      <SplitBar v-if="pvSplit.length" :title="t('reports.pvSplit')" :parts="pvSplit" />
    </div>
  </Card>
  <div class="grid grid-cols-1 gap-4 min-[1024px]:grid-cols-2">
    <Card v-if="hasBattery" :title="t('roles.battery')">
      <DataTable
        :label="t('roles.battery')"
        :header="[t('reports.figure'), t('reports.value')]"
        :rows="battery"
      />
      <DataTable
        v-if="r.batteries.length"
        class="mt-4"
        :label="t('roles.battery')"
        :header="[
          t('roles.battery'),
          t('reports.fig.charge'),
          t('reports.fig.discharge'),
          t('reports.ratio'),
        ]"
        :rows="r.batteries.map((m) => [m.label, kwh(m.in_kwh), kwh(m.out_kwh), ratio(m.ratio)])"
      />
    </Card>
    <Card v-if="r.pv_sources.length" :title="t('reports.pvSources')">
      <DataTable
        :label="t('reports.pvSources')"
        :header="[
          t('reports.source'),
          t('reports.fig.pv'),
          t('reports.share'),
          t('reports.specificYield'),
        ]"
        :rows="
          r.pv_sources.map((m) => [
            m.label,
            kwh(m.out_kwh),
            pct(m.share_pct),
            m.specific_yield == null
              ? '–'
              : `${formatNumber(m.specific_yield, lang, 1, 1)} kWh/kWp`,
          ])
        "
      />
    </Card>
    <Card v-if="lpRows.length" :title="t('reports.loadpoints')">
      <DataTable
        :label="t('reports.loadpoints')"
        :header="[
          t('reports.loadpoint'),
          t('reports.category'),
          t('reports.energy'),
          `${t('reports.solar')} ≈`,
          `${t('reports.solarShare')} ≈`,
        ]"
        :rows="lpRows"
      />
    </Card>
    <Card :title="t('reports.patterns')">
      <DataTable
        :label="t('reports.patterns')"
        :header="[t('reports.figure'), t('reports.value')]"
        :rows="patterns"
      />
    </Card>
  </div>
  <Card>
    <button
      type="button"
      class="flex min-h-11 w-full items-center justify-between gap-2 text-left"
      :aria-expanded="showTable"
      aria-controls="report-table"
      @click="showTable = !showTable"
    >
      <span>
        <span class="block text-lg font-semibold">{{ t('reports.table') }}</span>
        <span class="text-sm text-ink-2">{{ t('reports.tableRows', { n: tableRows.length }) }}</span>
      </span>
      <ChevronDown
        :size="20"
        class="shrink-0 text-ink-2 transition-transform"
        :class="showTable ? 'rotate-180' : ''"
        aria-hidden="true"
      />
    </button>
    <div v-if="showTable" id="report-table" class="mt-3">
      <DataTable :label="t('reports.table')" :header="tableHeader" :rows="tableRows" />
    </div>
  </Card>
  <Card :title="t('reports.quality')">
    <p class="text-sm">
      {{ t('reports.days', { n: s.complete_days, m: s.days }) }}
    </p>
    <p v-if="r.missing_days.length" class="mt-1 text-sm text-ink-2">
      {{ t('reports.missing') }}
      {{ r.missing_days.slice(0, 12).map(date).join(', ') }}
    </p>
    <p v-if="s.fallback_days" class="mt-1 text-sm text-ink-2">
      {{ t('reports.fallback', { n: s.fallback_days }, s.fallback_days) }}
    </p>
    <DataTable
      v-if="r.quality.length"
      class="mt-3"
      :label="t('reports.quality')"
      :header="[t('reports.meter'), t('reports.missingDays'), t('reports.estimatedDays')]"
      :rows="r.quality.map((q) => [q.label, String(q.missing_days), String(q.estimated_days)])"
    />
    <p class="mt-3 text-sm text-ink-2">{{ t('reports.estimates') }}</p>
  </Card>
</template>
