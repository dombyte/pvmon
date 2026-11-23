<script setup lang="ts">
// A date button that opens the themed calendar (spec 11: one look for every picker). The
// calendar is a modal dialog: centred on desktop, a bottom sheet in the compact layout.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { CalendarDays, X } from '@lucide/vue'
import { formatLocalDate, type Lang } from '@/format'
import type { LocalDate } from '@/format/localdate'
import { useLayout } from '@/layout/mode'
import CalendarGrid from './CalendarGrid.vue'
import PeriodGrid from './PeriodGrid.vue'
import { periodSelectable, type Unit } from './calendar'

const props = defineProps<{
  modelValue: LocalDate
  /** Accessible name and dialog heading. */
  label: string
  min?: LocalDate
  max?: LocalDate
  today?: LocalDate
  /** Other end of a range to highlight. */
  other?: LocalDate
  /** Button text; the medium date by default. */
  display?: string
  /** Pick a day (default), or a whole month or year (emitted as any day inside it). */
  unit?: Unit
}>()
const emit = defineEmits<{ 'update:modelValue': [LocalDate] }>()
// Two roots (button and dialog): classes go to the button.
defineOptions({ inheritAttrs: false })
const { t, locale } = useI18n()
const { mode } = useLayout()
const dialog = ref<HTMLDialogElement | null>(null)
const open = ref(false)
const text = computed(
  () => props.display ?? formatLocalDate(props.modelValue, locale.value as Lang),
)

const todayText = computed(() =>
  props.unit === 'month'
    ? t('date.thisMonth')
    : props.unit === 'year'
      ? t('date.thisYear')
      : t('day.today'),
)

function show(): void {
  open.value = true
  dialog.value?.showModal()
}
function close(): void {
  dialog.value?.close()
}
function pick(d: LocalDate): void {
  emit('update:modelValue', d)
  close()
}
</script>

<template>
  <button
    v-bind="$attrs"
    type="button"
    class="flex min-h-11 items-center justify-center gap-2 rounded-control border border-line bg-tile px-3 text-sm font-semibold tabular-nums hover:bg-well"
    aria-haspopup="dialog"
    :aria-label="`${label}: ${text}`"
    @click="show"
  >
    <CalendarDays :size="18" class="shrink-0 text-ink-2" aria-hidden="true" />
    <span>{{ text }}</span>
  </button>
  <dialog
    ref="dialog"
    class="picker glass rounded-card p-0 text-ink"
    :class="mode === 'compact' ? 'mx-0 mt-auto mb-0 w-full max-w-none rounded-b-none' : 'm-auto w-80'"
    :aria-label="label"
    @click.self="close"
    @close="open = false"
  >
    <div class="flex flex-col gap-2 p-4">
      <div class="flex items-center justify-between">
        <h2 class="text-base font-semibold">{{ label }}</h2>
        <button
          type="button"
          class="grid size-10 place-items-center rounded-control hover:bg-well"
          :aria-label="t('common.close')"
          @click="close"
        >
          <X :size="18" aria-hidden="true" />
        </button>
      </div>
      <PeriodGrid
        v-if="open && unit && unit !== 'day'"
        :unit="unit"
        :value="modelValue"
        :min="min"
        :max="max"
        :other="other"
        :today="today"
        @pick="pick"
      />
      <CalendarGrid
        v-else-if="open"
        :value="modelValue"
        :min="min"
        :max="max"
        :other="other"
        :today="today"
        @pick="pick"
      />
      <button
        v-if="today && periodSelectable(unit ?? 'day', today, min, max)"
        type="button"
        class="min-h-10 rounded-control border border-line text-sm hover:bg-well"
        @click="pick(today)"
      >
        {{ todayText }}
      </button>
    </div>
  </dialog>
</template>

<style scoped>
.picker::backdrop {
  background: rgb(0 0 0 / 0.35);
}
</style>
