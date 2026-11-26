<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Download } from '@lucide/vue'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'
import SelectField from '@/components/select/SelectField.vue'
import DateRangeField from '@/components/date/DateRangeField.vue'
import { formatLocalDate, type Lang } from '@/format'
import {
  compares,
  presetOf,
  presetRange,
  presets,
  type Compare,
  type Preset,
} from '@/reports/period'
import ReportBody from '@/reports/ReportBody.vue'
import { useReport } from '@/reports/useReport'

const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const { today, query, report, error, update, download, downloading, downloadError } = useReport(
  () => locale.value,
)
const active = computed(() =>
  query.value && today.value ? presetOf(query.value.from, query.value.to, today.value) : null,
)
const presetLabel = (p: Preset) => {
  switch (p) {
    case 'last_month':
      return t('reports.preset.lastMonth')
    case 'this_month':
      return t('reports.preset.thisMonth')
    case 'last_year':
      return t('reports.preset.lastYear')
    case 'this_year':
      return t('reports.preset.thisYear')
    default:
      return t('reports.preset.last30')
  }
}
const compareLabel = (c: Compare) =>
  c === 'previous'
    ? t('reports.compare.previous')
    : c === 'none'
      ? t('reports.compare.none')
      : t('reports.compare.lastYear')
const pick = (p: Preset) => update(presetRange(p, today.value))
const compareOptions = computed(() => compares.map((c) => ({ value: c, label: compareLabel(c) })))
const compare = computed<Compare>({
  get: () => query.value?.compare ?? 'last_year',
  set: (c) => update({ compare: c }),
})
const period = computed(() =>
  report.value
    ? `${formatLocalDate(report.value.from, lang.value)} – ${formatLocalDate(report.value.to, lang.value)}`
    : '',
)
</script>

<template>
  <Page :title="t('nav.reports')">
    <Card>
      <div class="flex flex-col gap-4">
        <div class="flex flex-wrap gap-2" role="group" :aria-label="t('reports.period')">
          <button
            v-for="p in presets"
            :key="p"
            type="button"
            class="min-h-10 rounded-control border border-line px-3 text-sm"
            :class="active === p ? 'toggle-on border-transparent' : 'text-ink-2'"
            :aria-pressed="active === p"
            @click="pick(p)"
          >
            {{ presetLabel(p) }}
          </button>
        </div>
        <div v-if="query" class="flex flex-wrap items-end gap-4">
          <div class="flex flex-col gap-1 text-sm">
            <span class="text-ink-2" aria-hidden="true">{{ t('reports.period') }}</span>
            <DateRangeField
              :from="query.from"
              :to="query.to"
              :label="t('reports.period')"
              :max="today"
              :today="today"
              @pick="(from, to) => update({ from, to })"
            />
          </div>
          <div class="flex flex-col gap-1 text-sm">
            <span class="text-ink-2" aria-hidden="true">{{ t('reports.compareLabel') }}</span>
            <SelectField v-model="compare" :options="compareOptions" :label="t('reports.compareLabel')" />
          </div>
          <div class="flex gap-2 min-[768px]:ml-auto">
            <button
              v-for="f in ['pdf', 'csv'] as const"
              :key="f"
              type="button"
              class="flex min-h-11 items-center gap-1.5 rounded-control bg-accent px-3 text-sm font-semibold text-on-accent disabled:opacity-60"
              :disabled="downloading !== null || !report"
              @click="download(f)"
            >
              <Download :size="16" aria-hidden="true" />
              {{ f === 'pdf' ? t('reports.pdf') : t('reports.csv') }}
            </button>
          </div>
        </div>
        <p v-if="downloadError" class="text-sm" style="color: var(--fault)" role="alert">
          {{ t(downloadError) }}
        </p>
      </div>
    </Card>
    <p v-if="error" class="text-sm" style="color: var(--fault)" role="alert">
      {{ t(error) }}
    </p>
    <template v-if="report">
      <p class="text-sm text-ink-2">
        {{ period }} ·
        {{
          t('reports.days', {
            n: report.summary.complete_days,
            m: report.summary.days,
          })
        }}
        <template v-if="report.today_excluded"> · {{ t('reports.todayExcluded') }} </template>
      </p>
      <ReportBody :report="report" />
    </template>
  </Page>
</template>
