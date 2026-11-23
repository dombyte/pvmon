<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DeviceState } from '@/api/types'

const props = defineProps<{ state: DeviceState }>()
const { t } = useI18n()
const color: Record<DeviceState, string> = {
  online: 'var(--battery)',
  stale: 'var(--notice)',
  offline: 'var(--fault)',
  unconfigured: 'var(--ink-2)',
}
const label = computed(() => {
  switch (props.state) {
    case 'online':
      return t('devices.state.online')
    case 'stale':
      return t('devices.state.stale')
    case 'offline':
      return t('devices.state.offline')
    default:
      return t('devices.state.unconfigured')
  }
})
</script>

<template>
  <span class="inline-flex items-center gap-1.5 text-sm">
    <span class="size-2.5 rounded-full" :style="{ background: color[state] }" aria-hidden="true" />
    {{ label }}
  </span>
</template>
