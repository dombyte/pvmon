<script setup lang="ts">
import { Lock } from '@lucide/vue'
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import FieldSelect from './FieldSelect.vue'
import TextField from '@/components/TextField.vue'
import type { BlockInfo, Body, DraftObject } from './draft'
import { useRoleLabel } from './roles'
import { useWizard } from './state'

// Step 3 (spec 11.14): one meter per profile block, proposed when the device was added;
// here blocks are switched on and off and roles chosen where the profile allows it.
const { t } = useI18n()
const w = useWizard()
const role = useRoleLabel()
const uid = useId()

interface Row {
  block: BlockInfo
  meter?: DraftObject
}

const devices = computed(() =>
  w.draft.value
    .filter((o) => o.kind === 'device')
    .map((d) => ({
      id: d.id,
      rows: w
        .blocksOf(d.id)
        .filter((b) => b.role !== 'phase')
        .map<Row>((b) => ({
          block: b,
          meter: w.draft.value.find(
            (o) => o.kind === 'meter' && o.body.device === d.id && o.body.source === b.name,
          ),
        })),
    })),
)

function roles(b: BlockInfo) {
  return [...new Set([b.role, ...b.roles_allowed])].map((r) => ({
    value: r,
    label: role(r),
  }))
}

function toggle(device: string, r: Row): void {
  if (r.meter) {
    w.dropMeter(r.meter.id)
    return
  }
  w.propose(device, [r.block])
}

function setRole(m: DraftObject, value: string): void {
  const body: Body = { ...m.body, role: value }
  if (value === 'consumer') {
    body.category ??= 'other'
    body.parent ??= 'site'
  } else {
    delete body.category
    if (body.parent === 'site') delete body.parent
  }
  w.setMeter({ ...m, body })
}

function setLabel(m: DraftObject, value: string): void {
  const body: Body = { ...m.body, label: value }
  if (!value) delete body.label
  w.setMeter({ ...m, body })
}

const str = (v: unknown) => (typeof v === 'string' ? v : '')
</script>

<template>
  <div class="flex flex-col gap-4">
    <p class="text-sm text-ink-2">{{ t('wizard.meters.intro') }}</p>
    <p v-if="devices.length === 0" class="text-sm">
      {{ t('wizard.devices.none') }}
    </p>
    <section
      v-for="d in devices"
      :key="d.id"
      class="flex flex-col gap-1"
      :aria-labelledby="`${uid}-${d.id}`"
    >
      <h3 :id="`${uid}-${d.id}`" class="text-sm font-semibold">{{ d.id }}</h3>
      <p v-if="d.rows.length === 0" class="text-sm text-ink-2">
        {{ t('wizard.meters.noBlocks') }}
      </p>
      <ul class="flex flex-col">
        <li
          v-for="r in d.rows"
          :key="r.block.name"
          class="grid items-center gap-2 border-t border-line py-2 first:border-t-0 min-[768px]:grid-cols-[minmax(10rem,1fr)_8rem_10rem_minmax(8rem,1fr)]"
        >
          <label class="flex min-h-11 items-center gap-2">
            <input
              type="checkbox"
              class="size-5 accent-accent"
              :checked="!!r.meter"
              :disabled="r.meter?.locked"
              @change="toggle(d.id, r)"
            />
            <span>
              <span class="font-medium">{{ r.block.name }}</span>
              <span class="text-xs text-ink-2"> · {{ role(r.block.role) }}</span>
            </span>
          </label>
          <template v-if="r.meter">
            <span class="flex items-center gap-1 text-sm">
              <Lock v-if="r.meter.locked" :size="12" aria-hidden="true" />{{ r.meter.id }}
            </span>
            <FieldSelect
              v-if="!r.meter.locked && roles(r.block).length > 1"
              :model-value="str(r.meter.body.role) || r.block.role"
              :options="roles(r.block)"
              :label="t('wizard.meters.role')"
              @update:model-value="(v: string) => setRole(r.meter!, v)"
            />
            <span v-else class="text-sm text-ink-2">{{
              role(str(r.meter.body.role) || r.block.role)
            }}</span>
            <TextField
              v-if="!r.meter.locked"
              :model-value="str(r.meter.body.label)"
              :label="t('wizard.meters.label')"
              @update:model-value="(v: string) => setLabel(r.meter!, v)"
            />
          </template>
        </li>
      </ul>
    </section>
  </div>
</template>
