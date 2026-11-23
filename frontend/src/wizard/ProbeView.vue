<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ProbeChannel, ProbeResult } from '@/api/types'
import { formatEnergy, formatNumber, formatPercent, formatPower, missing } from '@/format'
import { usePreferencesStore } from '@/stores/preferences'
import { useRoleLabel } from './roles'

// The normalised values of one probe, per profile block (spec 11.14 step 2).
defineProps<{ result: ProbeResult }>()
const { t } = useI18n()
const prefs = usePreferencesStore()
const role = useRoleLabel()
const valueDigits = 2

function value(c: ProbeChannel): string {
  const lang = prefs.language
  if (c.text) return c.text
  if (c.value === null) return missing
  if (c.unit === 'W') return formatPower(c.value, lang)
  if (c.unit === 'Wh') return formatEnergy(c.value, lang)
  if (c.unit === '%') return formatPercent(c.value, lang)
  return [formatNumber(c.value, lang, valueDigits, 0), c.unit].filter(Boolean).join(' ')
}
</script>

<template>
  <div class="flex flex-col gap-2 text-sm">
    <p v-if="result.error" class="text-fault" role="alert">
      {{ t('wizard.probe.failed', { error: result.error }) }}
    </p>
    <p v-else class="text-ink-2">
      {{
        t('wizard.probe.ok', {
          ms: formatNumber(result.took_ms, prefs.language),
        })
      }}
    </p>
    <div v-if="!result.error" class="grid gap-2 min-[640px]:grid-cols-2">
      <section
        v-for="b in result.blocks"
        :key="b.name"
        class="rounded-control bg-tile px-3 py-2"
        :aria-label="b.name"
      >
        <h3 class="mb-1 flex items-baseline justify-between gap-2 font-medium">
          <span>{{ b.name }}</span>
          <span class="text-xs text-ink-2">{{ role(b.role) }}</span>
        </h3>
        <dl class="grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
          <template v-for="c in b.channels" :key="c.name">
            <dt class="text-ink-2">{{ c.name }}</dt>
            <dd class="text-right tabular-nums">{{ value(c) }}</dd>
          </template>
        </dl>
      </section>
    </div>
  </div>
</template>
