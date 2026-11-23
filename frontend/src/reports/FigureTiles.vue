<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ReportFigure } from '@/api/types'
import { formatEnergy, formatNumber, formatPercent, type Lang } from '@/format'
import { figureLabel } from './labels'

const props = defineProps<{ figures: ReportFigure[]; compared: boolean }>()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const kwhToWh = 1000

const value = (f: ReportFigure, v: number | null) =>
  f.kind === 'ratio'
    ? formatPercent(v, lang.value, 1)
    : formatEnergy(v == null ? null : v * kwhToWh, lang.value)

/** "+12,3 %" for energy, "+4,0 Pkt." for ratios; the color says whether it is better. */
function change(f: ReportFigure): { text: string; color: string } {
  const v = f.kind === 'ratio' ? f.change : f.change_pct
  if (v == null) return { text: t('reports.noBase'), color: 'var(--ink-2)' }
  const shown = Math.round(v * 10) / 10 // the digit shown decides the color
  const sign = shown > 0 ? '+' : ''
  const text =
    f.kind === 'ratio'
      ? `${sign}${formatNumber(v, lang.value, 1, 1)} ${t('reports.points')}`
      : `${sign}${formatPercent(v, lang.value, 1)}`
  if (shown === 0 || f.better === 'neutral') return { text, color: 'var(--ink-2)' }
  const better = shown > 0 === (f.better === 'higher')
  return { text, color: better ? 'var(--better)' : 'var(--fault)' }
}
const tiles = computed(() =>
  props.figures.map((f) => ({
    id: f.id,
    label: figureLabel(t, f.id),
    value: value(f, f.value),
    change: change(f),
  })),
)
</script>

<template>
  <div class="grid grid-cols-2 gap-3 min-[1024px]:grid-cols-4">
    <div v-for="f in tiles" :key="f.id" class="rounded-tile bg-tile p-3">
      <p class="text-sm text-ink-2">{{ f.label }}</p>
      <p class="mt-1 text-xl font-semibold tabular-nums">{{ f.value }}</p>
      <p v-if="compared" class="mt-1 text-sm tabular-nums" :style="{ color: f.change.color }">
        {{ f.change.text }}
      </p>
    </div>
  </div>
</template>
