<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Card from '@/components/Card.vue'
import { formatEnergy, formatLocalDate, formatPercent, type Lang } from '@/format'
import { maxOf, share, type DayPoint, type TrendKey } from './trend'
import { tokenOf } from './trendOption'

/** Points newest first. */
const props = defineProps<{ points: DayPoint[]; keys: TrendKey[] }>()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)

const max = computed(() => maxOf(props.points, props.keys))
const open = ref<string | null>(null)
const toggle = (d: string) => (open.value = open.value === d ? null : d)

const names = computed<Record<TrendKey, string>>(() => ({
  pv: t('energy.pv'),
  consumption: t('energy.consumption'),
}))
const energy = (v: number | null) => formatEnergy(v, lang.value)
const day = (p: DayPoint) =>
  p.running
    ? t('trend.todayRunning')
    : formatLocalDate(p.date, lang.value, { weekday: 'short', day: 'numeric', month: 'short' })
</script>

<template>
  <Card>
    <div class="mb-1 flex items-center justify-between gap-2">
      <h2 class="text-lg font-semibold">{{ t('trend.lastDays', { n: points.length }) }}</h2>
      <span class="flex gap-3 text-xs text-ink-2">
        <span v-for="k in keys" :key="k" class="flex items-center gap-1">
          <span class="size-2.5 rounded-sm" :style="{ background: `var(--${tokenOf[k]})` }" />
          {{ names[k] }}
        </span>
      </span>
    </div>
    <ul>
      <li v-for="p in points" :key="p.date" class="border-b border-line py-3 last:border-0">
        <button
          type="button"
          class="w-full text-left"
          :aria-expanded="open === p.date"
          @click="toggle(p.date)"
        >
          <span class="mb-1.5 flex justify-between gap-2 text-sm">
            <span>
              <span class="font-semibold">{{ day(p) }}</span>
              <span v-if="p.incomplete" class="text-ink-2"> · {{ t('trend.incomplete') }}</span>
              <span v-if="p.estimated" class="text-ink-2"> · ≈ {{ t('energy.estimated') }}</span>
            </span>
            <span class="text-ink-2">
              {{ t('trend.selfSufficiency', { value: formatPercent(p.selfSufficiency, lang) }) }}
            </span>
          </span>
          <span v-for="k in keys" :key="k" class="mt-1 flex items-center gap-3">
            <span class="h-2 flex-1 overflow-hidden rounded-full bg-well">
              <span
                class="block h-full rounded-full"
                :style="{ width: `${share(p[k], max)}%`, background: `var(--${tokenOf[k]})` }"
              />
            </span>
            <span class="w-20 text-right text-sm text-ink-2">
              <span class="sr-only">{{ names[k] }}</span>
              {{ energy(p[k]) }}
            </span>
          </span>
        </button>
        <div v-if="open === p.date" class="mt-3 text-sm">
          <p class="flex justify-between gap-2 text-ink-2">
            <span>{{ t('energy.gridIn') }} {{ energy(p.gridIn) }}</span>
            <span>{{ t('energy.gridOut') }} {{ energy(p.gridOut) }}</span>
          </p>
          <RouterLink
            v-if="!p.running"
            :to="{ name: 'reports', query: { from: p.date, to: p.date } }"
            class="mt-3 flex min-h-11 items-center justify-center rounded-control bg-accent font-semibold text-on-accent"
          >
            {{ t('trend.openReport') }}
          </RouterLink>
        </div>
      </li>
    </ul>
  </Card>
</template>
