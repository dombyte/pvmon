<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { ChevronRight, TriangleAlert } from '@lucide/vue'
import { fetchDevices } from '@/api/client'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'
import { usePolled } from '@/composables/usePolled'
import StateBadge from '@/devices/StateBadge.vue'
import { usePollText } from '@/devices/usePollText'

const pollText = usePollText()
const { t } = useI18n()
/** Device states change with the health sweep; refresh like the status card. */
const refreshMs = 15_000
const { data, error } = usePolled(true, fetchDevices, refreshMs)
const devices = computed(() => data.value?.devices ?? [])
</script>

<template>
  <Page :title="t('nav.devices')">
    <p v-if="error" class="text-sm" style="color: var(--fault)">{{ t(error) }}</p>
    <div class="grid grid-cols-1 gap-4 min-[1024px]:grid-cols-2 min-[1440px]:grid-cols-3">
      <RouterLink
        v-for="d in devices"
        :key="d.id"
        :to="{ name: 'device', params: { id: d.id } }"
        class="rounded-card focus-visible:outline-2 focus-visible:outline-accent"
      >
        <Card class="h-full">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <h2 class="truncate text-lg font-semibold">{{ d.id }}</h2>
              <p class="truncate text-sm text-ink-2">{{ d.vendor }} · {{ d.models[0] ?? d.profile }}</p>
            </div>
            <ChevronRight :size="18" class="mt-1 shrink-0 text-ink-2" aria-hidden="true" />
          </div>
          <div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1">
            <StateBadge :state="d.state" />
            <span class="text-sm text-ink-2">{{ d.profile }}</span>
            <span class="text-sm text-ink-2">
              {{ t('devices.every', { n: d.interval_s }) }}<template v-if="d.last_poll">
                · {{ pollText(d.last_poll) }}</template>
            </span>
          </div>
          <p class="mt-2 text-sm text-ink-2">
            {{ t('devices.meters', { list: d.meters.join(', ') }) }}
          </p>
          <p v-if="!d.verified" class="mt-2 flex items-center gap-1.5 text-sm" style="color: var(--notice)">
            <TriangleAlert :size="16" aria-hidden="true" />
            {{ t('devices.unverified') }}
          </p>
        </Card>
      </RouterLink>
    </div>
  </Page>
</template>
