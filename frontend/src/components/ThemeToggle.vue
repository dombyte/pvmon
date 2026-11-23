<script setup lang="ts">
import { Monitor, Moon, Sun } from '@lucide/vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePreferencesStore, type ThemeChoice } from '@/stores/preferences'

const { t } = useI18n()
const prefs = usePreferencesStore()

const order: ThemeChoice[] = ['system', 'light', 'dark']
const icon = computed(() => ({ system: Monitor, light: Sun, dark: Moon })[prefs.theme])
const label = computed(() => t('theme.toggle', { current: t(`theme.${prefs.theme}`) }))

function cycle(): void {
  prefs.theme = order[(order.indexOf(prefs.theme) + 1) % order.length] ?? 'system'
}
</script>

<template>
  <button
    type="button"
    class="inline-flex size-11 items-center justify-center rounded-control text-ink-2 hover:bg-well"
    :aria-label="label"
    :title="label"
    @click="cycle"
  >
    <component :is="icon" :size="20" aria-hidden="true" />
  </button>
</template>
