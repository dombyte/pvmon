<script setup lang="ts">
// Month calendar of the date picker: Monday-first grid, month navigation, optional range
// highlight, keyboard support (arrows, Page Up/Down, Home/End, Enter/Space).
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ChevronRight } from '@lucide/vue'
import { formatLocalDate, type Lang } from '@/format'
import { addDays, type LocalDate } from '@/format/localdate'
import { clampDate, mondayRef, monthGrid, selectable, shiftMonth } from './calendar'

const props = defineProps<{
  value: LocalDate
  min?: LocalDate
  max?: LocalDate
  /** The other end of a range, highlighted between it and `value`. */
  other?: LocalDate
  today?: LocalDate
}>()
const emit = defineEmits<{ pick: [LocalDate] }>()
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
const lo = computed(() => (props.other && props.other < props.value ? props.other : props.value))
const hi = computed(() => (props.other && props.other > props.value ? props.other : props.value))
const inRange = (d: LocalDate) => !!props.other && d > lo.value && d < hi.value
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
    <div ref="grid" class="grid grid-cols-7 gap-y-1" role="group" :aria-label="title" @keydown="onKey">
      <button
        v-for="c in cells"
        :key="c.date"
        type="button"
        :data-date="c.date"
        class="relative mx-auto grid size-10 place-items-center rounded-full text-sm tabular-nums disabled:opacity-25"
        :class="[
          c.date === value || c.date === other
            ? 'bg-accent font-semibold text-on-accent'
            : inRange(c.date)
              ? 'toggle-range'
              : 'hover:bg-well',
          !c.inMonth && c.date !== value ? 'text-ink-2' : '',
          c.date === today && c.date !== value ? 'ring-1 ring-accent' : '',
        ]"
        :disabled="!selectable(c.date, min, max)"
        :tabindex="c.date === focused ? 0 : -1"
        :aria-label="label(c.date)"
        :aria-pressed="c.date === value"
        :aria-current="c.date === today ? 'date' : undefined"
        @click="pick(c.date)"
      >
        {{ Number(c.date.slice(8)) }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.toggle-range {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
}
</style>
