<script setup lang="ts">
// Month or year picker: a 3 × 4 grid of months (one year per page) or of years (12 per
// page), with the same look, range highlight and keyboard support as the day calendar.
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ChevronRight } from '@lucide/vue'
import { formatLocalDate, type Lang } from '@/format'
import type { LocalDate } from '@/format/localdate'
import { periodCells, periodOf, periodSelectable, shiftPeriod, yearsPerPage } from './calendar'

const props = defineProps<{
  unit: 'month' | 'year'
  /** Any day of the chosen period. */
  value: LocalDate
  min?: LocalDate
  max?: LocalDate
  other?: LocalDate
  today?: LocalDate
}>()
const emit = defineEmits<{ pick: [LocalDate] }>()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const cols = 3

const of = (d: LocalDate | undefined) => (d ? periodOf(props.unit, d) : '')
const value = computed(() => of(props.value))
const other = computed(() => of(props.other))
const today = computed(() => of(props.today))
const focused = ref(value.value || today.value)
const page = ref(focused.value)
watch(value, (v) => {
  if (!v) return
  focused.value = v
  page.value = v
})
const cells = computed(() => periodCells(props.unit, page.value))
const pageStep = computed(() => (props.unit === 'month' ? 12 : yearsPerPage))
const title = computed(() =>
  props.unit === 'month'
    ? page.value.slice(0, 4)
    : `${cells.value[0]!.slice(0, 4)} – ${cells.value.at(-1)!.slice(0, 4)}`,
)
const canPrev = computed(
  () => !props.min || periodSelectable(props.unit, shiftPeriod(props.unit, cells.value[0]!, -1), props.min),
)
const canNext = computed(
  () =>
    !props.max ||
    periodSelectable(props.unit, shiftPeriod(props.unit, cells.value.at(-1)!, 1), undefined, props.max),
)
function turn(dir: -1 | 1): void {
  page.value = shiftPeriod(props.unit, page.value, dir * pageStep.value)
}

const lo = computed(() => (other.value && other.value < value.value ? other.value : value.value))
const hi = computed(() => (other.value && other.value > value.value ? other.value : value.value))
const inRange = (d: LocalDate) => !!other.value && d > lo.value && d < hi.value
const text = (d: LocalDate) =>
  props.unit === 'month' ? formatLocalDate(d, lang.value, { month: 'short' }) : d.slice(0, 4)
const label = (d: LocalDate) =>
  props.unit === 'month'
    ? formatLocalDate(d, lang.value, { month: 'long', year: 'numeric' })
    : d.slice(0, 4)
const ok = (d: LocalDate) => periodSelectable(props.unit, d, props.min, props.max)

const grid = ref<HTMLElement | null>(null)
const moves: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -cols, ArrowDown: cols }
async function onKey(e: KeyboardEvent): Promise<void> {
  const n = moves[e.key]
  if (n === undefined) return
  e.preventDefault()
  const next = shiftPeriod(props.unit, focused.value, n)
  if (!ok(next)) return
  focused.value = next
  page.value = next
  await nextTick()
  grid.value?.querySelector<HTMLButtonElement>(`[data-date="${next}"]`)?.focus()
}
function pick(d: LocalDate): void {
  if (ok(d)) emit('pick', d)
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <div class="flex items-center justify-between">
      <button
        type="button"
        class="grid size-10 place-items-center rounded-control hover:bg-well disabled:opacity-30"
        :aria-label="unit === 'month' ? t('date.prevYear') : t('date.prevYears')"
        :disabled="!canPrev"
        @click="turn(-1)"
      >
        <ChevronLeft :size="18" aria-hidden="true" />
      </button>
      <p class="font-semibold tabular-nums" aria-live="polite">{{ title }}</p>
      <button
        type="button"
        class="grid size-10 place-items-center rounded-control hover:bg-well disabled:opacity-30"
        :aria-label="unit === 'month' ? t('date.nextYear') : t('date.nextYears')"
        :disabled="!canNext"
        @click="turn(1)"
      >
        <ChevronRight :size="18" aria-hidden="true" />
      </button>
    </div>
    <div ref="grid" class="grid grid-cols-3 gap-2" role="group" :aria-label="title" @keydown="onKey">
      <button
        v-for="c in cells"
        :key="c"
        type="button"
        :data-date="c"
        class="grid min-h-11 place-items-center rounded-control text-sm tabular-nums disabled:opacity-25"
        :class="[
          c === value || c === other
            ? 'bg-accent font-semibold text-on-accent'
            : inRange(c)
              ? 'toggle-range'
              : 'hover:bg-well',
          c === today && c !== value ? 'ring-1 ring-accent' : '',
        ]"
        :disabled="!ok(c)"
        :tabindex="c === focused ? 0 : -1"
        :aria-label="label(c)"
        :aria-pressed="c === value"
        :aria-current="c === today ? 'date' : undefined"
        @click="pick(c)"
      >
        {{ text(c) }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.toggle-range {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
}
</style>
