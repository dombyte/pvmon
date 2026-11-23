<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { CalendarDays, FileText, X } from '@lucide/vue'
import Card from '@/components/Card.vue'
import { useChart } from '@/charts/useChart'
import { formatEnergy, formatLocalDate, formatPercent, missing, type Lang } from '@/format'
import { sumOf, type DayPoint, type TrendKey } from './trend'
import { tokenOf, trendOption, type TrendText } from './trendOption'

const props = defineProps<{ points: DayPoint[]; keys: TrendKey[] }>()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)

const hidden = ref(new Set<TrendKey>())
const visible = computed(() => props.keys.filter((k) => !hidden.value.has(k)))
function toggle(k: TrendKey): void {
  const next = new Set(hidden.value)
  if (next.has(k)) next.delete(k)
  // The last visible series stays: an empty chart has nothing to scale or show.
  else if (visible.value.length > 1) next.add(k)
  hidden.value = next
}

const names = computed<Record<TrendKey, string>>(() => ({
  pv: t('energy.pv'),
  consumption: t('energy.consumption'),
}))
const text = computed<TrendText>(() => ({
  lang: lang.value,
  names: names.value,
  selfSufficiency: t('energy.selfSufficiency'),
  noData: t('trend.noData'),
  running: t('trend.running'),
  pickHint: t('trend.pickHint'),
}))

// A click pins a day below the chart: its links stay put instead of chasing the tooltip.
const selected = ref<number | null>(null)
const picked = computed(() => (selected.value === null ? null : props.points[selected.value]))
const el = ref<HTMLElement | null>(null)
const { failed } = useChart(
  el,
  (tokens, animate) =>
    props.points.length
      ? trendOption(props.points, visible.value, tokens, text.value, {
          animate,
          selected: selected.value,
        })
      : null,
  { onSelect: (i) => (selected.value = selected.value === i ? null : i) },
)

const summary = computed(() =>
  t('trend.summary', {
    n: props.points.length,
    values: props.keys
      .map((k) => `${names.value[k]} ${formatEnergy(sumOf(props.points, k), lang.value)}`)
      .join(', '),
  }),
)
const title = computed(() =>
  props.keys.includes('pv')
    ? t('trend.title', { n: props.points.length })
    : t('trend.titleConsumption', { n: props.points.length }),
)
const energy = (v: number | null) => formatEnergy(v, lang.value)
const day = (d: string) => formatLocalDate(d, lang.value, { weekday: 'short', day: 'numeric', month: 'short' })
</script>

<template>
  <Card :title="title">
    <div class="-mt-1 mb-2 flex items-center justify-between gap-2">
      <div class="flex gap-2">
        <button
          v-for="k in keys"
          :key="k"
          type="button"
          class="flex min-h-8 items-center gap-1.5 rounded-full px-2.5 text-sm"
          :class="hidden.has(k) ? 'toggle-off border border-line' : 'border border-line bg-tile'"
          :aria-pressed="!hidden.has(k)"
          @click="toggle(k)"
        >
          <span
            class="size-2.5 rounded-sm"
            :style="{ background: hidden.has(k) ? 'var(--line)' : `var(--${tokenOf[k]})` }"
          />
          {{ names[k] }}
        </button>
      </div>
      <span class="text-xs text-ink-2">{{ t('trend.unit') }}</span>
    </div>
    <p v-if="failed" class="text-sm text-ink-2">{{ t('errors.unknown') }}</p>
    <div ref="el" class="h-64 w-full cursor-pointer" role="img" :aria-label="summary" />
    <Transition name="pop">
      <div v-if="picked" class="mt-2 rounded-control bg-well p-3" aria-live="polite">
        <div class="flex items-center justify-between gap-2">
          <p class="text-sm font-semibold">
            {{ day(picked.date) }}<template v-if="picked.running"> · {{ t('trend.running') }}</template>
          </p>
          <button
            type="button"
            class="grid size-8 place-items-center rounded-full text-ink-2 hover:bg-tile"
            :aria-label="t('trend.close')"
            @click="selected = null"
          >
            <X :size="16" aria-hidden="true" />
          </button>
        </div>
        <p class="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-sm">
          <span v-for="k in keys" :key="k" class="flex items-center gap-1.5">
            <span class="size-2.5 rounded-sm" :style="{ background: `var(--${tokenOf[k]})` }" />
            <span class="text-ink-2">{{ names[k] }}</span>
            <span class="font-semibold">{{ energy(picked[k]) }}</span>
          </span>
          <span>
            <span class="text-ink-2">{{ t('energy.selfSufficiency') }}</span>
            <span class="font-semibold">
              {{ picked.selfSufficiency === null ? missing : formatPercent(picked.selfSufficiency, lang) }}
            </span>
          </span>
        </p>
        <div class="mt-3 grid grid-cols-2 gap-2">
          <RouterLink
            :to="{ name: 'day', query: picked.running ? {} : { date: picked.date } }"
            class="flex min-h-11 items-center justify-center gap-2 rounded-control border border-line bg-tile text-sm font-semibold hover:bg-well"
          >
            <CalendarDays :size="16" aria-hidden="true" />
            {{ t('trend.openDay') }}
          </RouterLink>
          <RouterLink
            v-if="!picked.running"
            :to="{ name: 'reports', query: { from: picked.date, to: picked.date } }"
            class="flex min-h-11 items-center justify-center gap-2 rounded-control bg-accent text-sm font-semibold text-on-accent"
          >
            <FileText :size="16" aria-hidden="true" />
            {{ t('trend.openReport') }}
          </RouterLink>
        </div>
      </div>
    </Transition>
    <!-- sr-only on the table itself leaks in some browsers: tables ignore overflow -->
    <div class="sr-only">
      <table>
        <caption>{{ summary }}</caption>
        <thead>
          <tr>
            <th scope="col">{{ t('trend.colDay') }}</th>
            <th v-for="k in keys" :key="k" scope="col">{{ names[k] }}</th>
            <th scope="col">{{ t('energy.selfSufficiency') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in points" :key="p.date">
            <th scope="row">{{ day(p.date) }}</th>
            <td v-for="k in keys" :key="k">{{ energy(p[k]) }}</td>
            <td>
              {{ p.selfSufficiency === null ? missing : formatPercent(p.selfSufficiency, lang) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="mt-2 text-xs text-ink-2">{{ t('trend.hint') }}</p>
  </Card>
</template>
