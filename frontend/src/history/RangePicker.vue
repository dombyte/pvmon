<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CalendarDays, Check, ChevronLeft, ChevronRight, X } from '@lucide/vue'
import DateField from '@/components/date/DateField.vue'
import { formatLocalDate, type Lang } from '@/format'
import { rangeLabel } from './labels'
import {
  atToday,
  bounds,
  customRange,
  granularities,
  presets,
  type Granularity,
  type Range,
} from './range'

const props = defineProps<{
  range: Range
  today: string
  phone: boolean
  /** The range is custom: no preset is marked. */
  custom?: boolean
  /** First day with data. */
  min?: string
}>()
const emit = defineEmits<{
  granularity: [Granularity]
  length: [number]
  move: [-1 | 1]
  custom: [Range]
}>()
const { t, locale } = useI18n()
const dialog = ref<HTMLDialogElement | null>(null)
const label = computed(() => rangeLabel(props.range, locale.value as Lang))
const granLabel = computed<Record<Granularity, string>>(() => ({
  day: t('history.days'),
  month: t('history.months'),
  year: t('history.years'),
}))
function presetLabel(n: number): string {
  if (props.range.gran === 'day') return t('history.last.day', { n })
  return props.range.gran === 'month' ? t('history.last.month', { n }) : t('history.last.year', { n })
}

function pick(n: number): void {
  emit('length', n)
  dialog.value?.close()
}

/** Custom range: two dates, read as their periods at the current granularity. */
const from = ref('')
const to = ref('')
watch(
  () => props.range,
  (r) => {
    from.value = bounds(r).from
    to.value = r.end
  },
  { immediate: true },
)
const fmt = computed<Intl.DateTimeFormatOptions>(() =>
  props.range.gran === 'day'
    ? { dateStyle: 'medium' }
    : props.range.gran === 'month'
      ? { month: 'short', year: 'numeric' }
      : { year: 'numeric' },
)
const show = (d: string) => formatLocalDate(d, locale.value as Lang, fmt.value)
function apply(): void {
  emit('custom', customRange(props.range.gran, from.value, to.value, props.today))
  dialog.value?.close()
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <div role="tablist" :aria-label="t('history.granularity')" class="grid grid-cols-3 gap-1 rounded-control bg-well p-1">
      <button
        v-for="g in granularities"
        :key="g"
        type="button"
        role="tab"
        :aria-selected="range.gran === g"
        class="min-h-10 rounded-chip text-sm"
        :class="range.gran === g ? 'toggle-on' : 'text-ink-2'"
        @click="emit('granularity', g)"
      >
        {{ granLabel[g] }}
      </button>
    </div>
    <div class="flex items-center gap-2">
      <button
        type="button"
        class="grid size-11 shrink-0 place-items-center rounded-control border border-line bg-tile"
        :aria-label="t('history.previous')"
        @click="emit('move', -1)"
      >
        <ChevronLeft :size="18" aria-hidden="true" />
      </button>
      <button
        type="button"
        class="flex min-h-11 flex-1 items-center justify-center gap-2 rounded-control border border-line bg-tile px-3 text-sm font-semibold tabular-nums"
        aria-haspopup="dialog"
        @click="dialog?.showModal()"
      >
        <CalendarDays :size="18" aria-hidden="true" />
        {{ label }}
      </button>
      <button
        type="button"
        class="grid size-11 shrink-0 place-items-center rounded-control border border-line bg-tile disabled:opacity-40"
        :aria-label="t('history.next')"
        :disabled="atToday(range, today)"
        @click="emit('move', 1)"
      >
        <ChevronRight :size="18" aria-hidden="true" />
      </button>
    </div>
    <dialog
      ref="dialog"
      class="picker glass rounded-card p-0 text-ink"
      :class="phone ? 'mx-0 mt-auto mb-0 w-full max-w-none rounded-b-none' : 'm-auto w-80'"
      :aria-label="t('history.chooseRange')"
      @click.self="dialog?.close()"
    >
      <div class="flex flex-col gap-2 p-4">
        <div class="flex items-center justify-between">
          <h2 class="text-base font-semibold">{{ t('history.chooseRange') }}</h2>
          <button
            type="button"
            class="grid size-11 place-items-center rounded-control hover:bg-well"
            :aria-label="t('common.close')"
            @click="dialog?.close()"
          >
            <X :size="18" aria-hidden="true" />
          </button>
        </div>
        <button
          v-for="n in presets[range.gran]"
          :key="n"
          type="button"
          class="flex min-h-11 items-center justify-between rounded-control px-3 text-left hover:bg-well"
          :class="!custom && range.n === n ? 'toggle-on' : ''"
          :aria-pressed="!custom && range.n === n"
          @click="pick(n)"
        >
          {{ presetLabel(n) }}
          <Check v-if="!custom && range.n === n" :size="18" aria-hidden="true" />
        </button>
        <div class="mt-2 flex flex-col gap-2 border-t border-line pt-3">
          <h3 class="text-sm font-semibold">{{ t('history.custom') }}</h3>
          <div class="grid grid-cols-2 gap-2">
            <div class="flex flex-col gap-1 text-xs text-ink-2">
              <span aria-hidden="true">{{ t('history.from') }}</span>
              <DateField
                v-model="from"
                :label="t('history.from')"
                :display="show(from)"
                :min="min"
                :max="today"
                :today="today"
                :other="to"
                :unit="range.gran"
              />
            </div>
            <div class="flex flex-col gap-1 text-xs text-ink-2">
              <span aria-hidden="true">{{ t('history.to') }}</span>
              <DateField
                v-model="to"
                :label="t('history.to')"
                :display="show(to)"
                :min="from"
                :max="today"
                :today="today"
                :other="from"
                :unit="range.gran"
              />
            </div>
          </div>
          <button
            type="button"
            class="min-h-11 rounded-control bg-accent text-sm font-semibold text-on-accent"
            @click="apply"
          >
            {{ t('history.apply') }}
          </button>
        </div>
      </div>
    </dialog>
  </div>
</template>

<style scoped>
.picker::backdrop {
  background: rgb(0 0 0 / 0.35);
}
</style>
