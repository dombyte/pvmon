<script setup lang="ts">
// The button that opens a date picker.
import { CalendarDays } from '@lucide/vue'

defineProps<{ label: string; text: string }>()
</script>

<template>
  <button
    type="button"
    class="flex min-h-11 items-center justify-center gap-2 rounded-control border border-line bg-tile px-3 text-sm font-semibold tabular-nums hover:bg-well"
    aria-haspopup="dialog"
    :aria-label="`${label}: ${text}`"
  >
    <CalendarDays :size="18" class="shrink-0 text-ink-2" aria-hidden="true" />
    <span>{{ text }}</span>
  </button>
</template>
