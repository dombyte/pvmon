<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnergyValue } from '@/api/types'
import { formatDayList, formatNumber, formatPercent, missing, type Lang } from '@/format'
import { missingDays, numberOf } from './rows'

const props = defineProps<{ value: EnergyValue | undefined; ratio: boolean; unit?: boolean }>()
const { t, locale } = useI18n()

const whPerKwh = 1000
const text = computed(() => {
  const n = numberOf(props.value)
  if (n === null) return missing
  const lang = locale.value as Lang
  if (props.ratio) return formatPercent(n, lang)
  const kwh = formatNumber(n / whPerKwh, lang, 1)
  return props.unit ? `${kwh} kWh` : kwh
})
const approx = computed(() => props.value?.estimated === true && numberOf(props.value) !== null)
const days = computed(() => missingDays([props.value]))
const incomplete = computed(() => days.value.length > 0)
const title = computed(() =>
  incomplete.value
    ? t('energy.incomplete', { days: formatDayList(days.value, locale.value as Lang) })
    : undefined,
)
</script>

<template>
  <span class="tabular-nums" :title="title">
    <span v-if="approx" :aria-label="t('energy.estimated')">≈</span>{{ text }}
    <span v-if="incomplete" class="text-xs text-ink-2">
      <span aria-hidden="true">*</span>
      <span class="sr-only">{{ title }}</span>
    </span>
  </span>
</template>
