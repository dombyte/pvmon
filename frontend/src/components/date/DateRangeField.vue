<script setup lang="ts">
// One button for a range: the first pick sets one end, the second the other (in any
// order). Until then the range to the hovered or focused cell is previewed.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight } from '@lucide/vue'
import { formatLocalDate, type Lang } from '@/format'
import type { LocalDate } from '@/format/localdate'
import { periodTitle } from '@/history/labels'
import CalendarGrid from './CalendarGrid.vue'
import PeriodGrid from './PeriodGrid.vue'
import PickerButton from './PickerButton.vue'
import PickerDialog from './PickerDialog.vue'
import { periodOf, sortRange, type Unit } from './calendar'

const props = defineProps<{
  from: LocalDate
  to: LocalDate
  /** Accessible name and dialog heading. */
  label: string
  min?: LocalDate
  max?: LocalDate
  today?: LocalDate
  /** Button text; "from – to" as medium dates by default. */
  display?: string
  /** Pick days (default), or whole months or years (emitted as their first day). */
  unit?: Unit
}>()
const emit = defineEmits<{ pick: [from: LocalDate, to: LocalDate] }>()
// Two roots (button and dialog): classes go to the button.
defineOptions({ inheritAttrs: false })
const { t, locale } = useI18n()
const dialog = ref<InstanceType<typeof PickerDialog> | null>(null)
const format = (d: LocalDate) => formatLocalDate(d, locale.value as Lang)
const text = computed(() => props.display ?? `${format(props.from)} – ${format(props.to)}`)

/** The first pick, while the second is still open. */
const anchor = ref<LocalDate | null>(null)
const hovered = ref<LocalDate | null>(null)
const grid = computed(() => ({
  value: anchor.value ?? props.to,
  other: anchor.value ? undefined : props.from,
  preview: anchor.value ? (hovered.value ?? anchor.value) : undefined,
  min: props.min,
  max: props.max,
  today: props.today,
}))

/** The two ends shown above the grid; the open one is marked. */
const ends = computed(() => {
  const unit = props.unit ?? 'day'
  const title = (d: LocalDate) => periodTitle(unit, d, locale.value as Lang)
  const start = anchor.value && periodOf(unit, anchor.value)
  const [a, b] = start
    ? hovered.value
      ? sortRange(start, hovered.value)
      : [start, null]
    : [props.from, props.to]
  // The marked box follows the date that moves: a preview before the anchor is the start.
  const backwards = !!start && !!hovered.value && hovered.value < start
  return [
    { name: t('date.start'), text: title(a), open: !start || backwards },
    { name: t('date.end'), text: b ? title(b) : '…', open: !!start && !backwards },
  ]
})

function show(): void {
  anchor.value = null
  dialog.value?.show()
}
function pick(d: LocalDate): void {
  if (!anchor.value) {
    anchor.value = d
    hovered.value = null
    return
  }
  // The month and year grids emit first days: compare the anchor at the same precision.
  emit('pick', ...sortRange(periodOf(props.unit ?? 'day', anchor.value), d))
  dialog.value?.close()
}
</script>

<template>
  <PickerButton v-bind="$attrs" :label="label" :text="text" @click="show" />
  <PickerDialog ref="dialog" :label="label">
    <p class="sr-only" aria-live="polite">
      {{ anchor ? t('date.pickEnd') : t('date.pickStart') }}
    </p>
    <div class="flex items-center gap-2">
      <template v-for="(e, i) in ends" :key="e.name">
        <ArrowRight v-if="i" :size="16" class="shrink-0 text-ink-2" aria-hidden="true" />
        <div
          class="flex-1 rounded-control px-3 py-1.5 transition-all duration-200"
          :class="e.open ? 'toggle-on' : 'bg-well'"
        >
          <p class="text-xs font-normal text-ink-2">{{ e.name }}</p>
          <p class="text-sm font-semibold tabular-nums">{{ e.text }}</p>
        </div>
      </template>
    </div>
    <PeriodGrid
      v-if="unit && unit !== 'day'"
      v-bind="grid"
      :unit="unit"
      @pick="pick"
      @hover="(d) => (hovered = d)"
    />
    <CalendarGrid v-else v-bind="grid" @pick="pick" @hover="(d) => (hovered = d)" />
  </PickerDialog>
</template>
