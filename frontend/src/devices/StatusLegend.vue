<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { StatusLevel } from '@/api/types'
import { levelColor } from './status'

const { t } = useI18n()
const levels: StatusLevel[] = ['ok', 'notice', 'fault', 'offline', 'none']
</script>

<template>
  <ul class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-ink-2" :aria-label="t('status.legend')">
    <li v-for="l in levels" :key="l" class="flex items-center gap-1.5">
      <span class="size-2.5 rounded-sm" :style="{ background: levelColor(l) }" aria-hidden="true" />
      {{ t(`status.level.${l}`) }}
    </li>
  </ul>
</template>
