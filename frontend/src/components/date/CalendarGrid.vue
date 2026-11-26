<script setup lang="ts">
// Month calendar of the date picker: Monday-first grid, month navigation, optional range
// highlight, keyboard support (arrows, Page Up/Down, Home/End, Enter/Space).
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ChevronRight } from '@lucide/vue'
import { formatLocalDate, type Lang } from '@/format'
import { addDays, type LocalDate } from '@/format/localdate'
import {
  bandOf,
  clampDate,
  mondayRef,
  monthGrid,
  selectable,
  shiftMonth,
  type Cell,
} from './calendar'
import { useSlide } from './slide'

const props = defineProps<{
  value: LocalDate
  min?: LocalDate
  max?: LocalDate
  /** The other end of a range, highlighted between it and `value`. */
  other?: LocalDate
  /** The still open end of a range: banded like `other`, but not picked yet. */
  preview?: LocalDate
  today?: LocalDate
}>()
const emit = defineEmits<{ pick: [LocalDate]; hover: [LocalDate] }>()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)

const focused = ref(clampDate(props.value || props.max || props.today || '', props.min, props.max))
const month = ref(focused.value)
watch(
  () => props.value,
  (v) => {
    if (!v) return
    focused.value = v
    month.value = v
  },
)
const cells = computed(() => monthGrid(month.value))
const title = computed(() =>
  formatLocalDate(`${month.value.slice(0, 7)}-01`, lang.value, { month: 'long', year: 'numeric' }),
)
const weekdays = computed(() =>
  Array.from({ length: 7 }, (_, i) =>
    formatLocalDate(addDays(mondayRef, i), lang.value, { weekday: 'short' }),
  ),
)
const canPrev = computed(() => !props.min || month.value.slice(0, 7) > props.min.slice(0, 7))
const canNext = computed(() => !props.max || month.value.slice(0, 7) < props.max.slice(0, 7))
const slide = useSlide(() => month.value.slice(0, 7))
// A range hides the days of other months, so that no day shows (or is banded) twice.
const ranged = computed(() => !!(props.other || props.preview))
const end = (d: LocalDate) => d === props.value || d === props.other
function wrap(c: Cell, i: number): string[] {
  const next = addDays(c.date, 1)
  return [
    `band band-${bandOf(c.date, props.value, props.preview ?? props.other)}`,
    i % 7 === 0 || c.date.endsWith('-01') ? 'cap-left' : '',
    i % 7 === 6 || next.endsWith('-01') ? 'cap-right' : '',
    ranged.value && !c.inMonth ? 'invisible' : '',
  ]
}
const label = (d: LocalDate) =>
  formatLocalDate(d, lang.value, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })

const grid = ref<HTMLElement | null>(null)
async function focusDate(d: LocalDate): Promise<void> {
  focused.value = clampDate(d, props.min, props.max)
  month.value = focused.value
  await nextTick()
  grid.value?.querySelector<HTMLButtonElement>(`[data-date="${focused.value}"]`)?.focus()
}
const moves: Record<string, (d: LocalDate) => LocalDate> = {
  ArrowLeft: (d) => addDays(d, -1),
  ArrowRight: (d) => addDays(d, 1),
  ArrowUp: (d) => addDays(d, -7),
  ArrowDown: (d) => addDays(d, 7),
  PageUp: (d) => shiftMonth(d, -1),
  PageDown: (d) => shiftMonth(d, 1),
  Home: (d) => `${d.slice(0, 7)}-01`,
  End: (d) => addDays(shiftMonth(d, 1), -1),
}
function onKey(e: KeyboardEvent): void {
  const move = moves[e.key]
  if (!move) return
  e.preventDefault()
  void focusDate(move(focused.value))
}
function pick(d: LocalDate): void {
  if (selectable(d, props.min, props.max)) emit('pick', d)
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <div class="flex items-center justify-between">
      <button
        type="button"
        class="grid size-10 place-items-center rounded-control hover:bg-well disabled:opacity-30"
        :aria-label="t('date.prevMonth')"
        :disabled="!canPrev"
        @click="month = shiftMonth(month, -1)"
      >
        <ChevronLeft :size="18" aria-hidden="true" />
      </button>
      <p class="font-semibold" aria-live="polite">{{ title }}</p>
      <button
        type="button"
        class="grid size-10 place-items-center rounded-control hover:bg-well disabled:opacity-30"
        :aria-label="t('date.nextMonth')"
        :disabled="!canNext"
        @click="month = shiftMonth(month, 1)"
      >
        <ChevronRight :size="18" aria-hidden="true" />
      </button>
    </div>
    <div class="grid grid-cols-7 text-center text-xs text-ink-2" aria-hidden="true">
      <span v-for="w in weekdays" :key="w" class="py-1">{{ w }}</span>
    </div>
    <div ref="grid" class="overflow-x-clip" role="group" :aria-label="title" @keydown="onKey">
      <div :key="month.slice(0, 7)" class="grid grid-cols-7 gap-y-1" :class="slide">
        <div v-for="(c, i) in cells" :key="c.date" :class="wrap(c, i)">
          <button
            type="button"
            :data-date="c.date"
            class="mx-auto grid size-10 place-items-center rounded-full text-sm tabular-nums disabled:opacity-25"
            :class="[
              end(c.date) ? 'range-end bg-accent font-semibold text-on-accent' : 'hover:bg-well',
              c.date === preview && !end(c.date) ? 'range-preview' : '',
              !c.inMonth && !end(c.date) ? 'text-ink-2' : '',
              c.date === today && !end(c.date) ? 'ring-1 ring-accent' : '',
            ]"
            :disabled="!selectable(c.date, min, max)"
            :tabindex="c.date === focused ? 0 : -1"
            :aria-label="label(c.date)"
            :aria-pressed="end(c.date)"
            :aria-current="c.date === today ? 'date' : undefined"
            @click="pick(c.date)"
            @mouseenter="emit('hover', c.date)"
            @focus="emit('hover', c.date)"
          >
            {{ Number(c.date.slice(8)) }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
