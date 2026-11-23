<script setup lang="ts">
// Shown while the UI runs on API answers the service worker kept (spec 11.11).
import { WifiOff } from '@lucide/vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime, type Lang } from '@/format'
import { offlineSince } from '@/pwa/offline'
import { useSiteStore } from '@/stores/site'

const { t, locale } = useI18n()
const site = useSiteStore()
const time = computed(() =>
  offlineSince.value && site.model
    ? formatDateTime(offlineSince.value, locale.value as Lang, site.model.tz)
    : '',
)
</script>

<template>
  <div
    v-if="offlineSince"
    role="status"
    class="flex min-h-11 items-center gap-2 bg-well px-4 text-sm"
  >
    <WifiOff :size="16" class="shrink-0 text-ink-2" aria-hidden="true" />
    {{ t('offline.banner', { time }) }}
  </div>
</template>
