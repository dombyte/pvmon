<script setup lang="ts">
// One bar per day in the colour of its worst level (spec 11.3 Status). With `link` every
// bar opens that day elsewhere; otherwise a click selects the day. In the compact layout
// day bars are too narrow to tap one by one: the strip is one 44 px target, the tap picks
// the day under the finger and arrow keys move the selection.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter, type RouteLocationRaw } from 'vue-router'
import { useLayout } from '@/layout/mode'
import type { StatusLevel } from '@/api/types'
import { formatDuration, formatLocalDate, type Lang } from '@/format'
import { levelColor, type DaySpan } from './status'

const props = defineProps<{
  levels: (StatusLevel | null)[]
  days: string[]
  label: string
  link?: (day: string) => RouteLocationRaw
  selected?: string
  /** How long each state lasted per day (shown on hover), and how to name a state. */
  spans?: DaySpan[][]
  spanLabel?: (s: DaySpan) => string
}>()
const emit = defineEmits<{ select: [day: string] }>()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const router = useRouter()
const { mode } = useLayout()
const compact = computed(() => mode.value === 'compact')

const bars = computed(() =>
  props.days.map((day, i) => {
    const level = props.levels[i] ?? null
    const date = formatLocalDate(day, lang.value, { day: 'numeric', month: 'short' })
    const title = `${date}: ${t(`status.level.${level ?? 'none'}`)}`
    // Screen readers get the durations the hover card shows.
    const spans = (props.spans?.[i] ?? [])
      .map((s) => `${props.spanLabel?.(s) ?? s.key} ${formatDuration(s.ms, lang.value)}`)
      .join(', ')
    return { day, level, title, spoken: spans ? `${title} (${spans})` : title }
  }),
)
/** The day under the pointer of a tap on the compact strip. */
function dayAt(ev: MouseEvent): string | undefined {
  const r = (ev.currentTarget as HTMLElement).getBoundingClientRect()
  const i = Math.floor(((ev.clientX - r.left) / r.width) * props.days.length)
  return props.days[Math.min(Math.max(i, 0), props.days.length - 1)]
}
function tap(ev: MouseEvent): void {
  const day = dayAt(ev)
  if (!day) return
  if (props.link) void router.push(props.link(day))
  else emit('select', day)
}
function key(ev: KeyboardEvent): void {
  const step = { ArrowLeft: -1, ArrowRight: 1 }[ev.key]
  if (!step || props.link) return
  ev.preventDefault()
  const i = props.selected ? props.days.indexOf(props.selected) : props.days.length
  const day = props.days[Math.min(Math.max(i + step, 0), props.days.length - 1)]
  if (day) emit('select', day)
}
/** Spoken summary of the strip: the selected day, or the days that were not OK. */
const spoken = computed(() => {
  const sel = bars.value.find((b) => b.day === props.selected)
  if (sel) return `${props.label}: ${sel.title}`
  const bad = bars.value.filter((b) => b.level && b.level !== 'ok').map((b) => b.title)
  return `${props.label}: ${bad.length ? bad.join(', ') : t('status.level.ok')}`
})
// Hover card of a day (desktop): every state of that day with its duration.
const hover = ref<number | null>(null)
const card = computed(() => {
  const i = hover.value
  const spans = i === null ? undefined : props.spans?.[i]
  if (i === null || !spans) return null
  const day = props.days[i] ?? ''
  return {
    date: formatLocalDate(day, lang.value, { weekday: 'short', day: 'numeric', month: 'short' }),
    // Kept inside the strip: anchored left near the start, right near the end.
    left: `${((i + 0.5) / props.days.length) * 100}%`,
    shift: i < props.days.length / 4 ? '-10%' : i > (props.days.length * 3) / 4 ? '-90%' : '-50%',
    rows: spans.map((s) => ({
      key: s.key,
      color: levelColor(s.level),
      label: props.spanLabel?.(s) ?? s.key,
      time: formatDuration(s.ms, lang.value),
    })),
  }
})
const first = computed(() =>
  props.days[0] ? formatLocalDate(props.days[0], lang.value, { day: 'numeric', month: 'short' }) : '',
)
</script>

<template>
  <div>
    <component
      :is="link ? 'a' : 'button'"
      v-if="compact"
      :type="link ? undefined : 'button'"
      :href="link ? router.resolve(link(days[days.length - 1] ?? '')).href : undefined"
      class="flex h-11 w-full cursor-pointer gap-px rounded-[3px]"
      :aria-label="spoken"
      @click.prevent="tap"
      @keydown="key"
    >
      <span
        v-for="b in bars"
        :key="b.day"
        class="my-2 flex-1 rounded-[2px]"
        :class="selected === b.day ? 'my-0' : ''"
        :style="{ background: levelColor(b.level) }"
      />
    </component>
    <div v-else class="relative" @pointerleave="hover = null">
      <Transition name="pop">
        <div
          v-if="card"
          class="glass-solid pointer-events-none absolute bottom-full z-20 mb-2 w-max max-w-64 rounded-control px-3 py-2 text-xs"
          :style="{ left: card.left, transform: `translateX(${card.shift})` }"
          aria-hidden="true"
        >
          <p class="mb-1 font-semibold">{{ card.date }}</p>
          <p v-if="!card.rows.length" class="text-ink-2">{{ t('status.level.none') }}</p>
          <p v-for="r in card.rows" :key="r.key" class="flex items-center gap-2">
            <span class="size-2 shrink-0 rounded-full" :style="{ background: r.color }" />
            <span class="truncate">{{ r.label }}</span>
            <span class="ml-auto pl-3 font-semibold">{{ r.time }}</span>
          </p>
        </div>
      </Transition>
      <ul class="flex h-7 gap-px" :aria-label="label">
        <li
          v-for="(b, i) in bars"
          :key="b.day"
          class="flex flex-1"
          @pointerenter="hover = i"
          @focusin="hover = i"
          @focusout="hover = null"
        >
          <RouterLink
            v-if="link"
            :to="link(b.day)"
            class="flex-1 rounded-[2px] hover:opacity-75 focus-visible:outline-2 focus-visible:outline-accent"
            :style="{ background: levelColor(b.level) }"
            :title="spans ? undefined : b.title"
            :aria-label="b.spoken"
          />
          <button
            v-else
            type="button"
            class="flex-1 rounded-[2px] hover:opacity-75"
            :class="selected === b.day ? 'ring-2 ring-accent ring-offset-1 ring-offset-tile' : ''"
            :style="{ background: levelColor(b.level) }"
            :title="spans ? undefined : b.title"
            :aria-label="b.spoken"
            :aria-pressed="selected === b.day"
            @click="emit('select', b.day)"
          />
        </li>
      </ul>
    </div>
    <div class="mt-1 flex justify-between text-xs text-ink-2">
      <span>{{ first }}</span>
      <span>{{ t('day.today') }}</span>
    </div>
  </div>
</template>
