<script setup lang="ts">
// Status page (spec 11.3 Status): every device with its generic level per day (ok,
// warning, fault, offline, no data). The register details live on the device page; a
// day opens it with that day's messages.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchStatusOverview } from '@/api/client'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'
import { usePolled } from '@/composables/usePolled'
import StateBadge from '@/devices/StateBadge.vue'
import StatusBars from '@/devices/StatusBars.vue'
import StatusLegend from '@/devices/StatusLegend.vue'
import { dayLevels, daySpans, levelColor, newest, type DaySpan } from '@/devices/status'
import { statusChoices as choices, useStatusDays } from '@/devices/useStatusDays'
import { formatDateTime, type Lang } from '@/format'
import { useSiteStore } from '@/stores/site'

const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const site = useSiteStore()
const { stored, n, today, days, desktop } = useStatusDays()
const refreshMs = 60_000
const { data, error } = usePolled(
  () => (days.value[0] ? { start: days.value[0], end: today.value } : null),
  fetchStatusOverview,
  refreshMs,
)

const rows = computed(() =>
  (site.model?.devices ?? []).map((d) => {
    const changes = data.value?.devices.find((s) => s.id === d.id)?.changes ?? []
    const last = newest(changes)
    const spans = site.model
      ? daySpans(
          changes.map((c) => ({ ...c, key: c.level })),
          days.value,
          site.model.tz,
          Date.now(),
        )
      : undefined
    return { id: d.id, state: d.state, levels: dayLevels(changes, days.value), spans, last }
  }),
)
const levelName = (s: DaySpan) => t(`status.level.${s.level}`)
const since = (at: string) =>
  t('status.since', { time: site.model ? formatDateTime(at, lang.value, site.model.tz) : at })
const dayLink = (id: string) => (day: string) => ({
  name: 'device',
  params: { id },
  query: { day },
  hash: '#status',
})
</script>

<template>
  <Page :title="t('nav.status')">
    <p class="-mt-3 text-sm text-ink-2">{{ t('status.subtitle', { n }) }}</p>
    <p v-if="error" class="text-sm" style="color: var(--fault)">{{ t(error) }}</p>
    <div class="flex flex-wrap items-center justify-between gap-3">
      <StatusLegend />
      <div v-if="desktop" class="flex gap-2" role="group" :aria-label="t('status.range')">
        <button
          v-for="c in choices"
          :key="c"
          type="button"
          class="min-h-10 rounded-control border border-line px-3 text-sm"
          :class="stored === c ? 'toggle-on border-transparent' : 'text-ink-2'"
          :aria-pressed="stored === c"
          @click="stored = c"
        >
          {{ t('status.days', { n: c }) }}
        </button>
      </div>
    </div>
    <Card>
      <p v-if="rows.length === 0" class="text-sm text-ink-2">{{ t('sidebar.noDevices') }}</p>
      <ul class="flex flex-col">
        <li v-for="r in rows" :key="r.id" class="border-t border-line py-3 first:border-t-0">
          <div class="mb-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
            <RouterLink
              :to="{ name: 'device', params: { id: r.id }, hash: '#status' }"
              class="inline-flex min-h-11 items-center font-semibold hover:underline"
            >
              {{ r.id }}
            </RouterLink>
            <p class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
              <StateBadge :state="r.state" />
              <span v-if="r.last" class="flex items-center gap-1.5">
                <span class="size-2.5 rounded-full" :style="{ background: levelColor(r.last.level) }" aria-hidden="true" />
                {{ t(`status.level.${r.last.level}`) }}
                <span class="text-ink-2">· {{ since(r.last.at) }}</span>
              </span>
            </p>
          </div>
          <StatusBars
            :levels="r.levels"
            :spans="r.spans"
            :span-label="levelName"
            :days="days"
            :label="t('status.history', { name: r.id })"
            :link="dayLink(r.id)"
          />
        </li>
      </ul>
    </Card>
  </Page>
</template>
