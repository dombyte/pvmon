<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLayout } from '@/layout/mode'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ChevronLeft, ChevronRight, Group, Ungroup } from '@lucide/vue'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'
import DateField from '@/components/date/DateField.vue'
import type { EChartsCoreOption } from 'echarts/core'
import { categoryToken, type Tokens } from '@/charts/tokens'
import { useChart } from '@/charts/useChart'
import {
  batteryOption,
  gridOption,
  powerOption,
  type DayText,
  type DayVisible,
  type LoadLine,
} from '@/day/dayOption'
import { useDay } from '@/day/useDay'
import { numberOf } from '@/energy/rows'
import { formatEnergy, formatLocalDate, formatPercent, type Lang } from '@/format'
import { addDays, parseLocalDate, todayIn } from '@/format/localdate'
import { useSiteStore } from '@/stores/site'

const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const site = useSiteStore()
const route = useRoute()
const router = useRouter()

const { mode } = useLayout()
const today = computed(() => (site.model ? todayIn(site.model.tz) : ''))
/** The date from ?date=, never after today; today without a valid one. */
const date = computed(() => {
  const q = route.query.date
  const d = typeof q === 'string' && parseLocalDate(q) ? q : today.value
  return d > today.value ? today.value : d
})
function go(d: string): void {
  void router.replace({ query: d === today.value ? {} : { date: d } })
}

const { req, slots, sums, quality, error } = useDay(
  computed(() => site.model),
  date,
)
const cats = computed(() => req.value?.categories ?? [])
const meters = computed(() => req.value?.meters ?? [])
const catName = (c: string) => t(`roles.${c}`)
const metersOf = (c: string) => meters.value.filter((m) => m.category === c)

type Chip = 'pv' | 'band' | 'consumption' | 'grid' | 'battery' | `lp:${string}`
/** The PV min/max band is a detail: off until asked for. */
const hidden = ref(new Set<Chip>(['band']))
function toggle(id: Chip): void {
  const next = new Set(hidden.value)
  if (!next.delete(id)) next.add(id)
  hidden.value = next
}
/** Loadpoint categories shown per meter instead of as one line. */
const split = ref(new Set<string>())
function toggleSplit(c: string): void {
  const next = new Set(split.value)
  if (!next.delete(c)) next.add(c)
  split.value = next
}

interface ChipItem {
  id: Chip
  name: string
  token: string
  /** Loadpoint category of the chip (with several meters it can be split). */
  cat?: string
}
const hasPV = computed(() => site.model?.site_series.includes('site/pv') ?? false)
const hasGrid = computed(() => site.model?.site_series.includes('site/grid_in') ?? false)
const hasBattery = computed(() => site.model?.site_series.includes('site/battery_in') ?? false)
const hasSoc = computed(() => site.model?.site_series.includes('site/soc') ?? false)
const chips = computed(() => {
  const out: ChipItem[] = []
  if (hasPV.value) {
    out.push({ id: 'pv', name: t('roles.pv'), token: 'pv' })
    out.push({ id: 'band', name: t('day.band'), token: 'pv' })
  }
  out.push({ id: 'consumption', name: t('energy.consumption'), token: 'home' })
  for (const c of cats.value) {
    if (!split.value.has(c)) {
      out.push({ id: `lp:${c}`, name: catName(c), token: categoryToken(c), cat: c })
      continue
    }
    for (const m of metersOf(c)) {
      out.push({ id: `lp:m:${m.id}`, name: m.label, token: categoryToken(c), cat: c })
    }
  }
  if (hasGrid.value) out.push({ id: 'grid', name: t('roles.grid'), token: 'grid' })
  if (hasBattery.value) out.push({ id: 'battery', name: t('roles.battery'), token: 'battery' })
  return out
})
const splitLabel = (c: string) =>
  split.value.has(c) ? t('day.join', { name: catName(c) }) : t('day.split', { name: catName(c) })
/** The split toggle follows the last chip of its category. */
const splitAfter = (i: number) => {
  const c = chips.value[i]?.cat
  return c && metersOf(c).length > 0 && chips.value[i + 1]?.cat !== c ? c : null
}
const vis = computed<DayVisible>(() => ({
  pv: !hidden.value.has('pv'),
  band: !hidden.value.has('band'),
  consumption: !hidden.value.has('consumption'),
  grid: !hidden.value.has('grid'),
  battery: !hidden.value.has('battery'),
}))

const dashes = ['solid', 'dashed', 'dotted'] as const
const loadLines = computed<LoadLine[]>(() =>
  cats.value.flatMap((c) => {
    const token = categoryToken(c)
    const lines: LoadLine[] = split.value.has(c)
      ? metersOf(c).map((m, i) => ({
          key: `m:${m.id}`,
          name: m.label,
          token,
          dash: dashes[i % dashes.length]!,
        }))
      : [{ key: c, name: catName(c), token, dash: 'solid' }]
    return lines.filter((l) => !hidden.value.has(`lp:${l.key}`))
  }),
)

const text = computed<DayText>(() => ({
  lang: lang.value,
  pv: t('day.pvAvg'),
  pvBand: t('day.pvBand'),
  consumption: t('energy.consumption'),
  loadpoints: loadLines.value,
  gridIn: t('energy.gridIn'),
  gridOut: t('energy.gridOut'),
  batteryOut: t('energy.batteryOut'),
  batteryIn: t('energy.batteryIn'),
  soc: t('day.soc'),
  noData: t('trend.noData'),
}))

const group = 'day'
const powerEl = ref<HTMLElement | null>(null)
const gridEl = ref<HTMLElement | null>(null)
const batteryEl = ref<HTMLElement | null>(null)
type Build = (tk: Tokens, a: boolean) => EChartsCoreOption
const build = (f: Build) => (tk: Tokens, a: boolean) => (slots.value.length ? f(tk, a) : null)
const p = useChart(powerEl, build((tk, a) => powerOption(slots.value, vis.value, tk, text.value, a)), { group })
useChart(gridEl, build((tk, a) => gridOption(slots.value, tk, text.value, a)), { group })
useChart(
  batteryEl,
  build((tk, a) => batteryOption(slots.value, hasSoc.value, tk, text.value, a)),
  { group },
)

const sum = (name: string) => numberOf(sums.value[name]?.values[0])
const sumRows = computed(() => {
  const s = site.model?.site_series ?? []
  const rows: { name: string; series: string; token: string }[] = [
    { name: t('energy.pv'), series: 'site/pv', token: 'pv' },
    { name: t('energy.consumption'), series: 'site/consumption', token: 'home' },
    ...cats.value.flatMap((c) => [
      {
        name: t('flow.ofWhich', { name: catName(c) }),
        series: `site/loadpoints/${c}`,
        token: categoryToken(c),
      },
      ...(split.value.has(c)
        ? metersOf(c).map((m) => ({ name: `– ${m.label}`, series: `${m.id}/energy_in`, token: categoryToken(c) }))
        : []),
    ]),
    { name: t('energy.gridIn'), series: 'site/grid_in', token: 'grid' },
    { name: t('energy.gridOut'), series: 'site/grid_out', token: 'grid' },
    { name: t('energy.batteryIn'), series: 'site/battery_in', token: 'battery' },
    { name: t('energy.batteryOut'), series: 'site/battery_out', token: 'battery' },
  ]
  return rows.filter((r) => s.includes(r.series) || r.series in sums.value)
})
const estimated = computed(() =>
  Object.values(sums.value).some((s) => s.values[0]?.estimated),
)
const title = computed(() =>
  date.value
    ? formatLocalDate(date.value, lang.value, {
        weekday: 'short',
        day: 'numeric',
        month: 'short',
        year: 'numeric',
      })
    : '',
)
const short = computed(() =>
  date.value ? formatLocalDate(date.value, lang.value, { day: 'numeric', month: 'short' }) : '',
)
const summary = computed(() =>
  t('day.summary', {
    date: title.value,
    pv: formatEnergy(sum('site/pv'), lang.value),
    consumption: formatEnergy(sum('site/consumption'), lang.value),
  }),
)
</script>

<template>
  <Page :title="t('nav.day')">
    <p class="-mt-3 text-sm text-ink-2">{{ t('day.subtitle') }}</p>
    <p v-if="site.error" class="text-sm" style="color: var(--fault)">{{ t(site.error) }}</p>
    <p v-if="error" class="text-sm" style="color: var(--fault)">{{ t(error) }}</p>
    <div v-if="site.model && date" class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="grid size-11 place-items-center rounded-control border border-line bg-tile"
        :aria-label="t('day.previous')"
        @click="go(addDays(date, -1))"
      >
        <ChevronLeft :size="18" aria-hidden="true" />
      </button>
      <DateField
        class="min-w-48 flex-1 sm:flex-none"
        :model-value="date"
        :label="t('day.pick')"
        :display="title"
        :max="today"
        :min="site.model.data_since ?? undefined"
        :today="today"
        @update:model-value="go"
      />
      <button
        type="button"
        class="grid size-11 place-items-center rounded-control border border-line bg-tile disabled:opacity-40"
        :aria-label="t('day.next')"
        :disabled="date >= today"
        @click="go(addDays(date, 1))"
      >
        <ChevronRight :size="18" aria-hidden="true" />
      </button>
      <button
        type="button"
        class="min-h-11 rounded-control border border-line bg-tile px-4 text-sm disabled:opacity-40"
        :disabled="date >= today"
        @click="go(today)"
      >
        {{ t('day.today') }}
      </button>
    </div>
    <div v-if="site.model && date" class="grid grid-cols-1 gap-4 min-[1024px]:grid-cols-3">
      <Card class="min-[1024px]:col-span-2 min-[1024px]:row-span-2">
        <div class="mb-3 flex flex-wrap items-center gap-2">
          <template v-for="(c, i) in chips" :key="c.id">
            <button
              type="button"
              class="flex min-h-8 items-center gap-1.5 rounded-full border border-line px-2.5 text-sm"
              :class="hidden.has(c.id) ? 'toggle-off' : 'bg-tile'"
              :aria-pressed="!hidden.has(c.id)"
              @click="toggle(c.id)"
            >
              <span
                class="size-2.5 rounded-full"
                :class="c.id === 'band' ? 'opacity-40' : ''"
                :style="{ background: hidden.has(c.id) ? 'var(--line)' : `var(--${c.token})` }"
              />
              {{ c.name }}
            </button>
            <button
              v-if="splitAfter(i)"
              type="button"
              class="grid size-8 place-items-center rounded-full border border-line text-ink-2 hover:bg-well"
              :aria-pressed="split.has(splitAfter(i)!)"
              :title="splitLabel(splitAfter(i)!)"
              :aria-label="splitLabel(splitAfter(i)!)"
              @click="toggleSplit(splitAfter(i)!)"
            >
              <Group v-if="split.has(splitAfter(i)!)" :size="16" aria-hidden="true" />
              <Ungroup v-else :size="16" aria-hidden="true" />
            </button>
          </template>
        </div>
        <p v-if="p.failed.value" class="text-sm text-ink-2">{{ t('errors.unknown') }}</p>
        <div class="flex items-baseline justify-between">
          <h2 class="font-semibold">{{ t('day.powerTitle') }}</h2>
          <span class="text-xs text-ink-2">kW</span>
        </div>
        <div ref="powerEl" class="mt-2 h-64 w-full" role="img" :aria-label="summary" />
        <div v-if="hasGrid" v-show="vis.grid">
          <div class="mt-4 flex items-baseline justify-between gap-2">
            <h2 class="font-semibold">{{ t('day.gridTitle') }}</h2>
            <span class="text-xs text-ink-2">{{ t('day.gridUnit') }}</span>
          </div>
          <div ref="gridEl" class="mt-2 h-40 w-full" role="img" :aria-label="t('day.gridTitle')" />
        </div>
        <div v-if="hasBattery" v-show="vis.battery">
          <div class="mt-4 flex items-baseline justify-between gap-2">
            <h2 class="font-semibold">{{ t('day.batteryTitle') }}</h2>
            <span class="text-xs text-ink-2">
              {{ hasSoc ? t('day.batteryUnitSoc') : t('day.batteryUnit') }}
            </span>
          </div>
          <div ref="batteryEl" class="mt-2 h-40 w-full" role="img" :aria-label="t('day.batteryTitle')" />
        </div>
        <p class="mt-3 text-xs text-ink-2">{{ mode === 'desktop' ? t('day.hint') : t('day.hintTouch') }}</p>
      </Card>
      <Card :title="t('day.sum', { date: short })">
        <dl class="flex flex-col">
          <div
            v-for="r in sumRows"
            :key="r.series"
            class="flex min-h-11 items-center justify-between border-t border-line first:border-t-0"
          >
            <dt class="flex items-center gap-2">
              <span class="size-2.5 rounded-full" :style="{ background: `var(--${r.token})` }" />
              {{ r.name }}
            </dt>
            <dd class="font-semibold tabular-nums">{{ formatEnergy(sum(r.series), lang) }}</dd>
          </div>
          <div class="flex min-h-11 items-center justify-between border-t border-line">
            <dt class="flex items-center gap-2">
              <span class="size-2.5 rounded-full bg-ink-2" />
              {{ t('energy.selfSufficiency') }}
            </dt>
            <dd class="font-semibold tabular-nums">
              {{ formatPercent(sum('site/self_sufficiency'), lang) }}
            </dd>
          </div>
        </dl>
        <RouterLink
          v-if="date < today"
          :to="{ name: 'reports', query: { from: date, to: date } }"
          class="mt-3 flex min-h-11 items-center justify-center rounded-control border border-line bg-tile text-sm font-semibold"
        >
          {{ t('trend.openReport') }}
        </RouterLink>
      </Card>
      <Card :title="t('day.quality')">
        <p>{{ t('day.buckets', { n: quality.complete, m: quality.total }) }}</p>
        <p class="mt-1 text-sm text-ink-2">
          {{ estimated ? t('day.estimated') : t('day.measured') }}
        </p>
      </Card>
      <div class="sr-only">
        <table>
          <caption>{{ summary }}</caption>
          <thead>
            <tr>
              <th scope="col">{{ t('day.colTime') }}</th>
              <th scope="col">{{ t('energy.pv') }}</th>
              <th scope="col">{{ t('energy.consumption') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in slots" :key="s.at">
              <th scope="row">{{ s.time }}</th>
              <td>{{ s.pv ?? '–' }}</td>
              <td>{{ s.consumption ?? '–' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </Page>
</template>
