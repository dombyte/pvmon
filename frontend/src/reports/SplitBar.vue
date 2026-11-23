<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatEnergy, formatPercent, type Lang } from '@/format'

export interface SplitPart {
  label: string
  color: string
  kwh: number
  pct: number | null
}

const props = defineProps<{ title: string; parts: SplitPart[] }>()
const { locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const kwhToWh = 1000
const empty = computed(() => props.parts.every((p) => p.pct == null))
</script>

<template>
  <div>
    <h3 class="mb-2 font-semibold">{{ title }} ≈</h3>
    <div
      class="flex h-3 gap-0.5 overflow-hidden rounded-full bg-well"
      role="img"
      :aria-label="title"
    >
      <template v-if="!empty">
        <span
          v-for="p in parts"
          :key="p.label"
          :style="{ width: `${p.pct ?? 0}%`, background: p.color }"
        />
      </template>
    </div>
    <ul class="mt-2 flex flex-wrap gap-x-6 gap-y-1 text-sm">
      <li v-for="p in parts" :key="p.label" class="flex items-center gap-1.5">
        <span class="size-2.5 rounded-full" :style="{ background: p.color }" aria-hidden="true" />
        <span class="font-semibold">{{ p.label }}</span>
        <span class="tabular-nums text-ink-2">
          {{ formatEnergy(p.kwh * kwhToWh, lang) }} ·
          {{ formatPercent(p.pct, lang, 1) }}
        </span>
      </li>
    </ul>
  </div>
</template>
