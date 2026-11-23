<script setup lang="ts">
import { CircleAlert, CircleCheck } from '@lucide/vue'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatPower } from '@/format'
import { usePreferencesStore } from '@/stores/preferences'
import { balance, blockPower, type DraftObject } from './draft'
import { useRoleLabel } from './roles'
import { probeOf, useWizard } from './state'

// Step 5 (spec 11.14): every device is read once more; each meter shows its power and the
// balance PV + grid + battery ≈ consumption checks the signs.
const { t } = useI18n()
const w = useWizard()
const prefs = usePreferencesStore()
const role = useRoleLabel()
const busy = ref(false)

const devices = computed(() => w.draft.value.filter((o) => o.kind === 'device').map((d) => d.id))
const meters = computed(() => w.draft.value.filter((o) => o.kind === 'meter'))

async function measure(): Promise<void> {
  busy.value = true
  for (const id of devices.value) await w.probe(id)
  busy.value = false
}
onMounted(() => void measure())

function power(m: DraftObject): number | null {
  return blockPower(probeOf(w, String(m.body.device)), String(m.body.source))
}
const result = computed(() => balance(w.draft.value, power, w.roleOf))
const ok = computed(() => result.value.problems.length === 0 && result.value.missing.length === 0)
const fmt = (v: number | null) => formatPower(v, prefs.language)

/** Why a device gave no values: its own error or the failed request. */
function failure(id: string): string | null {
  const p = w.probes.value[id]
  if (!p) return null
  if ('failed' in p) return t(p.failed)
  return p.error ?? null
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center gap-3">
      <p class="flex-1 text-sm text-ink-2">{{ t('wizard.check.intro') }}</p>
      <button
        type="button"
        class="min-h-11 rounded-control border border-line px-3 text-sm disabled:opacity-60"
        :disabled="busy"
        @click="measure"
      >
        {{ t('wizard.check.again') }}
      </button>
    </div>
    <p v-if="busy" class="text-sm text-ink-2" role="status">
      {{ t('wizard.probe.reading') }}
    </p>
    <template v-for="id in devices" :key="id">
      <p v-if="failure(id)" class="text-sm text-fault" role="alert">
        {{ t('wizard.check.deviceFailed', { device: id, error: failure(id) }) }}
      </p>
    </template>
    <table class="w-full text-sm">
      <thead class="text-left text-ink-2">
        <tr>
          <th class="py-1 font-normal">{{ t('wizard.check.meter') }}</th>
          <th class="py-1 font-normal">{{ t('wizard.meters.role') }}</th>
          <th class="py-1 text-right font-normal">
            {{ t('wizard.check.power') }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in meters" :key="m.id" class="border-t border-line">
          <td class="py-1.5">{{ m.id }}</td>
          <td class="py-1.5 text-ink-2">{{ role(w.roleOf(m)) }}</td>
          <td class="py-1.5 text-right tabular-nums">{{ fmt(power(m)) }}</td>
        </tr>
      </tbody>
    </table>
    <div
      v-if="!busy && meters.length"
      class="flex flex-col gap-2 rounded-control bg-tile p-3 text-sm"
    >
      <p>
        {{
          t('wizard.check.sum', {
            pv: fmt(result.pv),
            grid: fmt(result.grid),
            battery: fmt(result.battery),
            consumption: fmt(result.consumption),
          })
        }}
      </p>
      <p v-if="ok" class="flex items-center gap-1.5">
        <CircleCheck :size="16" class="text-accent" aria-hidden="true" />{{ t('wizard.check.ok') }}
      </p>
      <p v-if="result.problems.includes('negative')" class="flex items-start gap-1.5 text-fault">
        <CircleAlert :size="16" class="mt-0.5 shrink-0" aria-hidden="true" />{{
          t('wizard.check.negative')
        }}
      </p>
      <p v-if="result.problems.includes('loads')" class="flex items-start gap-1.5 text-fault">
        <CircleAlert :size="16" class="mt-0.5 shrink-0" aria-hidden="true" />
        {{ t('wizard.check.loads', { loads: fmt(result.loads) }) }}
      </p>
      <p v-if="result.missing.length" class="text-ink-2">
        {{ t('wizard.check.missing', { meters: result.missing.join(', ') }) }}
      </p>
    </div>
  </div>
</template>
