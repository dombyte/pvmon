<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import EnergyCell from '@/energy/EnergyCell.vue'
import { kpiRows, type EnergyRow } from '@/energy/rows'
import type { EnergyValue } from '@/api/types'

const props = defineProps<{
  rows: EnergyRow[]
  phone: boolean
  value: (name: string) => EnergyValue | undefined
}>()
const { t } = useI18n()
const tiles = computed(() => kpiRows(props.rows, props.phone))
const color = (r: EnergyRow) => (r.color === 'neutral' ? 'var(--ink-2)' : `var(--${r.color})`)
</script>

<template>
  <div class="grid grid-cols-2 gap-3">
    <div v-for="r in tiles" :key="r.name" class="glass rounded-card p-4">
      <p class="flex items-center gap-2 text-[13px] text-ink-2">
        <span class="size-3.5 shrink-0 rounded-full border-2" :style="{ borderColor: color(r) }" />
        {{ t('energy.today', { name: t(r.labelKey) }) }}
      </p>
      <p class="mt-1 text-[22px] font-semibold md:text-[28px]">
        <EnergyCell :value="value(r.name)" :ratio="r.ratio" unit />
      </p>
    </div>
  </div>
</template>
