<script setup lang="ts">
import { computed, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute } from 'vue-router'
import { ChevronLeft, TriangleAlert } from '@lucide/vue'
import { fetchDevice } from '@/api/client'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'
import { usePolled } from '@/composables/usePolled'
import { formatChannel } from '@/devices/channel'
import DeviceStatus from '@/devices/DeviceStatus.vue'
import StateBadge from '@/devices/StateBadge.vue'
import { usePollText } from '@/devices/usePollText'
import { formatDateTime, type Lang } from '@/format'
import { useSiteStore } from '@/stores/site'
import { useSubscription } from '@/live/useSubscription'

const pollText = usePollText()
const { t, locale } = useI18n()
const lang = computed(() => locale.value as Lang)
const route = useRoute()
const id = computed(() => String(route.params.id ?? ''))
const site = useSiteStore()
const refreshMs = 15_000
const { data: device, error } = usePolled(id, fetchDevice, refreshMs)
const keys = computed(() => device.value?.blocks.flatMap((b) => b.channels.map((c) => c.key)) ?? [])
const { values } = useSubscription(keys)

// A link from the status page (#status) scrolls to the status once the device is loaded.
let scrolled = false
watch(device, async (d) => {
  if (!d || scrolled || route.hash !== '#status') return
  scrolled = true
  await nextTick()
  document.getElementById('status')?.scrollIntoView({ block: 'start' })
})
</script>

<template>
  <Page :title="device?.id ?? id">
    <RouterLink :to="{ name: 'devices' }" class="-mt-3 flex items-center gap-1 text-sm text-accent">
      <ChevronLeft :size="16" aria-hidden="true" />{{ t('nav.devices') }}
    </RouterLink>
    <p v-if="error" class="text-sm" style="color: var(--fault)">{{ t(error) }}</p>
    <div v-if="device" class="grid grid-cols-1 gap-4 min-[1024px]:grid-cols-3">
      <Card :title="t('devices.info')">
        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt class="text-ink-2">{{ t('devices.stateLabel') }}</dt>
          <dd><StateBadge :state="device.state" /></dd>
          <dt class="text-ink-2">{{ t('devices.profile') }}</dt>
          <dd>{{ device.profile }}</dd>
          <dt class="text-ink-2">{{ t('devices.model') }}</dt>
          <dd>{{ device.vendor }} · {{ device.models.join(', ') }}</dd>
          <dt class="text-ink-2">{{ t('devices.transport') }}</dt>
          <dd>{{ device.transport }}{{ device.read ? ` · ${device.read}` : '' }}</dd>
          <dt class="text-ink-2">{{ t('devices.interval') }}</dt>
          <dd>{{ t('devices.seconds', { n: device.interval_s }) }}</dd>
          <template v-if="device.last_poll">
            <dt class="text-ink-2">{{ t('devices.lastPoll') }}</dt>
            <dd>
              <span :style="device.last_poll.ok ? undefined : { color: 'var(--fault)' }">
                {{ pollText(device.last_poll) }}
              </span>
              <span v-if="site.model" class="text-ink-2">
                · {{ formatDateTime(device.last_poll.at, lang, site.model.tz, { timeStyle: 'medium' }) }}
              </span>
            </dd>
          </template>
          <template v-if="device.netting">
            <dt class="text-ink-2">{{ t('devices.netting') }}</dt>
            <dd>{{ device.netting }}</dd>
          </template>
        </dl>
        <p v-if="!device.verified" class="mt-3 flex items-start gap-1.5 text-sm" style="color: var(--notice)">
          <TriangleAlert :size="16" class="mt-0.5 shrink-0" aria-hidden="true" />
          {{ t('devices.unverifiedLong') }}
        </p>
      </Card>
      <Card
        v-for="b in device.blocks"
        :key="b.meter"
        :title="`${b.label} · ${t(`roles.${b.role}`)}`"
      >
        <dl class="flex flex-col">
          <div
            v-for="c in b.channels"
            :key="c.key"
            class="flex min-h-9 items-center justify-between gap-3 border-t border-line first:border-t-0 text-sm"
          >
            <dt class="text-ink-2">{{ c.name }}</dt>
            <dd class="font-semibold tabular-nums">
              {{ formatChannel(values[c.key]?.value, values[c.key]?.unit ?? c.unit, lang) }}
            </dd>
          </div>
        </dl>
      </Card>
      <DeviceStatus :device="device" class="min-[1024px]:col-span-3" />
    </div>
  </Page>
</template>
