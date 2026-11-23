<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'
import { formatDateTime, type Lang } from '@/format'
import { useLayout } from '@/layout/mode'
import { useSubscription } from '@/live/useSubscription'
import { useOverviewEnergy } from '@/energy/useOverviewEnergy'
import Breakdown from '@/overview/Breakdown.vue'
import EnergyTable from '@/overview/EnergyTable.vue'
import KpiTiles from '@/overview/KpiTiles.vue'
import LastDays from '@/overview/LastDays.vue'
import EnergyFlow from '@/overview/EnergyFlow.vue'
import PowerNow from '@/overview/PowerNow.vue'
import TrendChart from '@/overview/TrendChart.vue'
import { listDays } from '@/overview/trend'
import { useTrend } from '@/overview/useTrend'
import { overviewKeys, powerState, type Lookup, type RolePower } from '@/overview/power'
import { useNodeLabel } from '@/overview/useNodeLabel'
import { useSessionStore } from '@/stores/session'
import { useSiteStore } from '@/stores/site'

const { t, locale } = useI18n()
const site = useSiteStore()
const { phone } = useLayout()
const label = useNodeLabel()

/** No meter yet: the first start before the setup wizard (spec 3, 11.14). */
const empty = computed(() => site.model?.meters.length === 0)
const session = useSessionStore()
const model = computed(() => (empty.value ? null : site.model))

const keys = computed(() => (model.value ? overviewKeys(model.value) : []))
const { values } = useSubscription(keys)
const get: Lookup = (key) => {
  const v = values.value[key]?.value
  return typeof v === 'number' ? v : undefined
}
const state = computed(() => (model.value ? powerState(model.value, get) : null))

/** Newest timestamp of any live value: "as of" in the header. */
const asOf = computed(() => {
  let newest = ''
  for (const v of Object.values(values.value)) if (v?.ts && v.ts > newest) newest = v.ts
  if (!newest || !site.model) return ''
  const lang = locale.value as Lang
  const tz = site.model.tz
  return t('overview.asOf', {
    date: formatDateTime(newest, lang, tz, { weekday: 'long', day: 'numeric', month: 'short', year: 'numeric' }),
    time: formatDateTime(newest, lang, tz, { timeStyle: 'medium' }),
  })
})

const energy = useOverviewEnergy(model)
const today = (name: string) => energy.value(name, 'today')
const trend = useTrend(model)
const lastDays = computed(() => trend.points.value.slice(-listDays).reverse())
const errorKey = computed(() => energy.error.value ?? trend.error.value)

const selected = ref<RolePower | null>(null)
const selectedLabel = computed(() =>
  selected.value && state.value ? label(selected.value, state.value.soc, state.value.socApprox) : '',
)
</script>

<template>
  <Page :title="t('nav.overview')">
    <p v-if="asOf" class="-mt-3 text-sm text-ink-2">{{ asOf }}</p>
    <p v-if="site.error" class="text-sm" style="color: var(--fault)">{{ t(site.error) }}</p>
    <p v-if="errorKey" class="text-sm" style="color: var(--fault)">
      {{ t(errorKey) }}
    </p>
    <Card v-if="empty" :title="t('overview.empty.title')">
      <p class="text-sm text-ink-2">{{ t('overview.empty.text') }}</p>
      <RouterLink
        v-if="session.user"
        :to="{ name: 'wizard' }"
        class="mt-3 inline-flex min-h-11 items-center rounded-control bg-accent px-4 text-sm font-medium text-on-accent"
      >
        {{ t('overview.empty.start') }}
      </RouterLink>
      <RouterLink
        v-else
        :to="{ name: 'login', query: { next: '/wizard' } }"
        class="mt-3 inline-block text-sm text-accent underline"
      >
        {{ t('overview.empty.login') }}
      </RouterLink>
    </Card>
    <div v-if="state && model" class="grid grid-cols-1 gap-4 min-[1440px]:grid-cols-3">
      <PowerNow v-if="phone" :state="state" @select="selected = $event" />
      <Card v-else :title="t('flow.title')" class="min-[1440px]:col-span-2">
        <EnergyFlow :state="state" @select="selected = $event" />
      </Card>
      <KpiTiles :rows="energy.rows.value" :phone="phone" :value="today" />
      <EnergyTable
        :rows="energy.rows.value"
        :phone="phone"
        :value="energy.value"
        class="min-[1440px]:col-span-2"
      />
      <LastDays v-if="phone" :points="lastDays" :keys="trend.keys.value" />
      <TrendChart v-else :points="trend.points.value" :keys="trend.keys.value" />
      <Breakdown
        :node="selected"
        :label="selectedLabel"
        :site="model"
        :get="get"
        @close="selected = null"
      />
    </div>
  </Page>
</template>
