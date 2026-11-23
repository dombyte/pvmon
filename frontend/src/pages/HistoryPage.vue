<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'
import { useChart } from '@/charts/useChart'
import { useEnergy } from '@/energy/useEnergy'
import { formatEnergy, formatPercent, missing, type Lang } from '@/format'
import { todayIn } from '@/format/localdate'
import { historyOption, type HistoryText } from '@/history/historyOption'
import { axisLabel, periodTitle, rangeLabel } from '@/history/labels'
import {
  historyKeys,
  historySeries,
  periodPoints,
  rangeSum,
  tokenOf,
  type HistoryKey,
} from '@/history/points'
import { bounds, periodLastDay, type Granularity } from '@/history/range'
import RangePicker from '@/history/RangePicker.vue'
import { useHistoryRange } from '@/history/useHistoryRange'
import { useLayout } from '@/layout/mode'
import { useSiteStore } from '@/stores/site'

const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const site = useSiteStore()
const { phone } = useLayout()

const today = computed(() => (site.model ? todayIn(site.model.tz) : ''))
const { range, custom, setGranularity, setLength, setCustom, move } = useHistoryRange(today)
const keys = computed(() => (site.model ? historyKeys(site.model) : []))
const { series, error } = useEnergy(() =>
  site.model && range.value
    ? { series: historySeries(site.model), level: range.value.gran, ...bounds(range.value) }
    : null,
)
const points = computed(() => (range.value ? periodPoints(series.value, range.value) : []))
const sum = computed(() => rangeSum(points.value, keys.value))

const hidden = ref(new Set<HistoryKey>())
const visible = computed(() => keys.value.filter((k) => !hidden.value.has(k)))
function toggle(k: HistoryKey): void {
  const next = new Set(hidden.value)
  if (next.has(k)) next.delete(k)
  // The last visible series stays: an empty chart has nothing to scale or show.
  else if (visible.value.length > 1) next.add(k)
  hidden.value = next
}

/** Selected period; a new range selects its newest one. */
const selected = ref<number | null>(null)
watch(range, () => (selected.value = null), { deep: true })
const current = computed(() => points.value[selected.value ?? points.value.length - 1] ?? null)

const names = computed<Record<HistoryKey, string>>(() => ({
  pv: t('energy.pv'),
  consumption: t('energy.consumption'),
  gridIn: t('energy.gridIn'),
  gridOut: t('energy.gridOut'),
}))
const gran = computed<Granularity>(() => range.value?.gran ?? 'day')
const text = computed<HistoryText>(() => ({
  lang: lang.value,
  names: names.value,
  noData: t('trend.noData'),
  axis: (p) => axisLabel(gran.value, p.start, lang.value),
  title: (p) => periodTitle(gran.value, p.start, lang.value),
}))

const el = ref<HTMLElement | null>(null)
const { failed } = useChart(
  el,
  (tokens, animate) =>
    points.value.length
      ? historyOption(points.value, visible.value, selected.value, tokens, text.value, animate)
      : null,
  { onSelect: (i) => (selected.value = i) },
)

/** Minimum width per bar slot (spec 11.7); wider ranges scroll, starting at the newest. */
const slotPx: Record<Granularity, number> = { day: 18, month: 26, year: 40 }
const scroller = ref<HTMLElement | null>(null)
const minWidth = computed(() => `${points.value.length * slotPx[gran.value]}px`)
watch(
  () => [points.value.length, gran.value],
  () => void nextTick(() => scroller.value?.scrollTo({ left: scroller.value.scrollWidth })),
)

/** The period containing today is still running: no report yet. */
const running = computed(() => {
  const p = current.value
  return !!p && periodLastDay(gran.value, p.start, today.value) === today.value
})
const reportTo = computed(() => {
  const p = current.value
  if (!p || running.value) return null
  return { name: 'reports', query: { from: p.start, to: periodLastDay(gran.value, p.start, today.value) } }
})
const energy = (v: number | null) => formatEnergy(v, lang.value)
const percent = (v: number | null) => (v === null ? missing : formatPercent(v, lang.value))
const summary = computed(() =>
  range.value
    ? t('history.summary', {
        range: rangeLabel(range.value, lang.value),
        values: keys.value.map((k) => `${names.value[k]} ${energy(sum.value.values[k])}`).join(', '),
      })
    : '',
)
</script>

<template>
  <Page :title="t('nav.history')">
    <p class="-mt-3 text-sm text-ink-2">{{ t('history.subtitle') }}</p>
    <p v-if="site.error" class="text-sm" style="color: var(--fault)">{{ t(site.error) }}</p>
    <p v-if="error" class="text-sm" style="color: var(--fault)">{{ t(error) }}</p>
    <div v-if="range && site.model" class="grid grid-cols-1 gap-4 min-[1024px]:grid-cols-3">
      <Card class="min-[1024px]:col-span-2">
        <RangePicker
          :range="range"
          :today="today"
          :phone="phone"
          :custom="custom"
          :min="site.model.data_since ?? undefined"
          @granularity="setGranularity"
          @length="setLength"
          @custom="setCustom"
          @move="move"
        />
        <div class="mt-3 flex flex-wrap gap-2">
          <button
            v-for="k in keys"
            :key="k"
            type="button"
            class="flex min-h-8 items-center gap-1.5 rounded-full border border-line px-2.5 text-sm"
            :class="hidden.has(k) ? 'toggle-off' : 'bg-tile'"
            :aria-pressed="!hidden.has(k)"
            @click="toggle(k)"
          >
            <span
              class="size-2.5 rounded-full"
              :style="{ background: hidden.has(k) ? 'var(--line)' : `var(--${tokenOf[k]})` }"
            />
            {{ names[k] }}
          </button>
        </div>
        <section
          v-if="current"
          class="mt-3 rounded-control border border-line bg-tile p-3"
          aria-live="polite"
        >
          <div class="mb-2 flex items-baseline justify-between gap-2">
            <h2 class="font-semibold">
              {{ periodTitle(gran, current.start, lang) }}
              <span v-if="running" class="font-normal text-ink-2">· {{ t('trend.running') }}</span>
            </h2>
            <span v-if="current.selfSufficiency !== null" class="text-sm text-ink-2">
              {{ t('energy.selfSufficiency') }} {{ percent(current.selfSufficiency) }}
            </span>
          </div>
          <dl class="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
            <div v-for="k in keys" :key="k" class="flex items-center justify-between gap-2">
              <dt class="flex items-center gap-1.5 text-ink-2">
                <span class="size-2 rounded-full" :style="{ background: `var(--${tokenOf[k]})` }" />
                {{ names[k] }}
              </dt>
              <dd class="font-semibold tabular-nums">{{ energy(current.values[k]) }}</dd>
            </div>
          </dl>
          <p v-if="current.incomplete || current.estimated" class="mt-1 text-xs text-ink-2">
            {{ current.incomplete ? t('history.incomplete') : t('energy.estimated') }}
          </p>
          <RouterLink v-if="reportTo" :to="reportTo" class="mt-2 inline-block text-sm text-accent underline">
            {{ t('history.openReport') }}
          </RouterLink>
        </section>
        <p v-if="failed" class="mt-3 text-sm text-ink-2">{{ t('errors.unknown') }}</p>
        <div
          ref="scroller"
          class="mt-3 overflow-x-auto rounded-chip"
          tabindex="0"
          :aria-label="t('history.chart')"
        >
          <div ref="el" class="h-64 w-full" :style="{ minWidth }" role="img" :aria-label="summary" />
        </div>
        <p class="mt-2 text-xs text-ink-2">{{ phone ? t('history.hintTouch') : t('history.hint') }}</p>
      </Card>
      <Card :title="t('history.sum', { range: rangeLabel(range, lang) })">
        <dl class="flex flex-col">
          <div
            v-for="k in keys"
            :key="k"
            class="flex min-h-11 items-center justify-between border-t border-line first:border-t-0"
          >
            <dt class="flex items-center gap-2">
              <span class="size-2.5 rounded-full" :style="{ background: `var(--${tokenOf[k]})` }" />
              {{ names[k] }}
            </dt>
            <dd class="font-semibold tabular-nums">{{ energy(sum.values[k]) }}</dd>
          </div>
          <div class="flex min-h-11 items-center justify-between border-t border-line">
            <dt class="flex items-center gap-2">
              <span class="size-2.5 rounded-full bg-ink-2" />
              {{ t('energy.selfSufficiency') }}
            </dt>
            <dd class="font-semibold tabular-nums">{{ percent(sum.selfSufficiency) }}</dd>
          </div>
        </dl>
        <p v-if="sum.empty > 0" class="mt-2 text-xs text-ink-2">
          {{ t('history.withoutData', { n: sum.empty, m: sum.total }) }}
        </p>
      </Card>
      <div class="sr-only">
        <table>
          <caption>{{ summary }}</caption>
          <thead>
            <tr>
              <th scope="col">{{ t('history.colPeriod') }}</th>
              <th v-for="k in keys" :key="k" scope="col">{{ names[k] }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in points" :key="p.start">
              <th scope="row">{{ periodTitle(gran, p.start, lang) }}</th>
              <td v-for="k in keys" :key="k">{{ energy(p.values[k]) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </Page>
</template>
