<script setup lang="ts">
// Status of one device (spec 11.3 Devices, 12): the connection and every status register
// of its profile as day bars, register chips to choose what is shown (stored per device
// and browser), and the messages of the selected day or the newest ones.
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { fetchStatus } from '@/api/client'
import { connectionRegister, type DeviceDetail, type StatusEvent } from '@/api/types'
import Card from '@/components/Card.vue'
import { readStored, writeStored } from '@/composables/storage'
import { usePolled } from '@/composables/usePolled'
import { formatDateTime, formatLocalDate, type Lang } from '@/format'
import { useSiteStore } from '@/stores/site'
import StatusBars from './StatusBars.vue'
import StatusLegend from './StatusLegend.vue'
import { daySpans, levelColor, newest, registerDays } from './status'
import { useStatusDays } from './useStatusDays'

const props = defineProps<{ device: DeviceDetail }>()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const route = useRoute()
const router = useRouter()
const site = useSiteStore()
const { today, days } = useStatusDays()

const refreshMs = 15_000
const { data } = usePolled(
  () => (days.value[0] ? { id: props.device.id, start: days.value[0], end: today.value } : null),
  fetchStatus,
  refreshMs,
)
const events = computed(() => data.value?.events ?? [])

/** Registers hidden by the user, stored per device. */
const storageKey = computed(() => `pvmon.device.${props.device.id}.hidden`)
const hidden = ref(new Set<string>())
watch(
  storageKey,
  (k) => {
    try {
      hidden.value = new Set(JSON.parse(readStored(k) ?? '[]') as string[])
    } catch {
      hidden.value = new Set()
    }
  },
  { immediate: true },
)
function toggle(name: string): void {
  const next = new Set(hidden.value)
  if (!next.delete(name)) next.add(name)
  hidden.value = next
  writeStored(storageKey.value, JSON.stringify([...next]))
}

const label = (name: string) => {
  if (name === connectionRegister) return t('status.connection')
  const l = props.device.status.find((r) => r.name === name)?.label
  return l?.[lang.value] ?? l?.en ?? name
}
const registers = computed(() => [connectionRegister, ...props.device.status.map((r) => r.name)])
const shown = computed(() => registers.value.filter((r) => !hidden.value.has(r)))
const levels = computed(() => registerDays(events.value, shown.value, days.value))

/** Text of a change: the connection by its level, else the profile texts. */
function message(e: StatusEvent): string {
  if (e.register === connectionRegister) {
    if (e.level === 'ok') return t('status.online')
    return e.level === 'offline' ? t('status.level.offline') : t('status.stopped')
  }
  const texts = (e.texts ?? []).map((x) => x[lang.value] ?? x.en ?? '').join(', ')
  return texts || t(`status.level.${e.level}`)
}
const rows = computed(() =>
  shown.value.map((name) => {
    const mine = events.value.filter((e) => e.register === name)
    const last = newest(mine)
    // Hover: how long each message lasted that day.
    const spans = site.model
      ? daySpans(
          mine.map((e) => ({ at: e.at, level: e.level, key: message(e) })),
          days.value,
          site.model.tz,
          Date.now(),
        )
      : undefined
    return { name, label: label(name), levels: levels.value[name] ?? [], spans, last }
  }),
)

const day = computed(() => (typeof route.query.day === 'string' ? route.query.day : ''))
const select = (d: string) =>
  void router.replace({ query: d === day.value ? {} : { day: d }, hash: '#status' })
const logSize = 50
const log = computed(() => {
  const mine = events.value.filter((e) => !hidden.value.has(e.register))
  const sorted = [...mine].sort((a, b) => b.at.localeCompare(a.at))
  if (day.value) return sorted.filter((e) => e.at.slice(0, 10) === day.value)
  return sorted.filter((e) => e.at.slice(0, 10) >= (days.value[0] ?? '')).slice(0, logSize)
})
const when = (at: string) => (site.model ? formatDateTime(at, lang.value, site.model.tz) : at)
const dayLabel = computed(() =>
  day.value ? formatLocalDate(day.value, lang.value, { weekday: 'short', day: 'numeric', month: 'short' }) : '',
)
</script>

<template>
  <Card id="status" :title="t('status.device')" class="scroll-mt-4">
    <p class="-mt-1 mb-3 text-sm text-ink-2">{{ t('status.deviceHint') }}</p>
    <div class="mb-3 flex flex-wrap gap-2" role="group" :aria-label="t('status.registers')">
      <button
        v-for="r in registers"
        :key="r"
        type="button"
        class="flex min-h-8 items-center rounded-full border border-line px-2.5 text-sm"
        :class="hidden.has(r) ? 'toggle-off' : 'bg-tile'"
        :aria-pressed="!hidden.has(r)"
        @click="toggle(r)"
      >
        {{ label(r) }}
      </button>
    </div>
    <ul class="flex flex-col">
      <li v-for="r in rows" :key="r.name" class="border-t border-line py-3 first:border-t-0">
        <div class="mb-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-sm">
          <span class="font-semibold">{{ r.label }}</span>
          <span v-if="r.last" class="flex items-center gap-1.5">
            <span class="size-2.5 rounded-full" :style="{ background: levelColor(r.last.level) }" aria-hidden="true" />
            {{ message(r.last) }}
          </span>
        </div>
        <StatusBars
          :levels="r.levels"
          :spans="r.spans"
          :days="days"
          :label="t('status.history', { name: r.label })"
          :selected="day"
          @select="select"
        />
      </li>
    </ul>
    <div class="mt-3"><StatusLegend /></div>
    <div class="mt-5 flex flex-wrap items-center justify-between gap-2">
      <h3 class="font-semibold">{{ day ? t('status.dayLog', { day: dayLabel }) : t('status.allLog') }}</h3>
      <button
        v-if="day"
        type="button"
        class="min-h-9 rounded-control border border-line px-3 text-sm"
        @click="select(day)"
      >
        {{ t('status.clearDay') }}
      </button>
    </div>
    <p v-if="log.length === 0" class="mt-2 text-sm text-ink-2">
      {{ day ? t('status.noneDay') : t('devices.noStatus') }}
    </p>
    <ul class="mt-1 flex flex-col">
      <li
        v-for="e in log"
        :key="`${e.register}-${e.at}`"
        class="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-line py-2 text-sm first:border-t-0"
      >
        <span class="w-40 tabular-nums text-ink-2">{{ when(e.at) }}</span>
        <span class="w-36 font-semibold">{{ label(e.register) }}</span>
        <span class="flex flex-1 items-center gap-1.5">
          <span class="size-2.5 shrink-0 rounded-full" :style="{ background: levelColor(e.level) }" aria-hidden="true" />
          {{ message(e) }}
        </span>
      </li>
    </ul>
  </Card>
</template>
