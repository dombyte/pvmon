<script setup lang="ts">
import { Lock } from '@lucide/vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import FieldSelect from './FieldSelect.vue'
import TextField from '@/components/TextField.vue'
import type { DraftObject } from './draft'
import { categories, useRoleLabel } from './roles'
import { useWizard } from './state'

// Step 4 (spec 11.14): category and parent of every consumer, the PV total of a string,
// peak power and battery capacity.
const { t } = useI18n()
const w = useWizard()
const role = useRoleLabel()

const meters = computed(() => w.draft.value.filter((o) => o.kind === 'meter'))
const shown = computed(() =>
  meters.value.filter((m) => ['consumer', 'pv', 'battery'].includes(w.roleOf(m))),
)
const str = (v: unknown) => (typeof v === 'string' || typeof v === 'number' ? String(v) : '')

const categoryOptions = computed(() => categories.map((c) => ({ value: c, label: role(c) })))

/** Consumers sit below the site or another consumer; strings below a PV total. */
function parents(m: DraftObject) {
  if (w.roleOf(m) === 'pv') {
    const totals = meters.value.filter(
      (o) => w.roleOf(o) === 'pv' && !o.body.parent && o.id !== m.id,
    )
    return [
      { value: '', label: t('wizard.hierarchy.topLevel') },
      ...totals.map((o) => ({ value: o.id, label: o.id })),
    ]
  }
  const others = meters.value.filter((o) => w.roleOf(o) === 'consumer' && o.id !== m.id)
  return [
    { value: 'site', label: t('wizard.hierarchy.site') },
    ...others.map((o) => ({ value: o.id, label: o.id })),
  ]
}

function set(m: DraftObject, field: string, value: string): void {
  const body = { ...m.body, [field]: value }
  if (value === '') delete body[field]
  w.setMeter({ ...m, body })
}

function setNumber(m: DraftObject, field: 'kwp' | 'capacity', value: string): void {
  const n = Number(value.replace(',', '.'))
  const body = { ...m.body }
  if (value.trim() === '' || !Number.isFinite(n)) delete body[field]
  else body[field] = field === 'capacity' ? `${n}kWh` : n
  w.setMeter({ ...m, body })
}

/** Capacity in kWh for the input ("10kWh" → "10"). */
function kWh(v: unknown): string {
  const m = /^([\d.]+)\s*kWh$/.exec(str(v))
  return m ? m[1]! : str(v)
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <p class="text-sm text-ink-2">{{ t('wizard.hierarchy.intro') }}</p>
    <p v-if="shown.length === 0" class="text-sm">
      {{ t('wizard.hierarchy.none') }}
    </p>
    <ul class="flex flex-col">
      <li
        v-for="m in shown"
        :key="m.id"
        class="grid items-end gap-2 border-t border-line py-3 first:border-t-0 min-[768px]:grid-cols-[minmax(8rem,1fr)_12rem_12rem]"
      >
        <span class="flex min-h-11 flex-col justify-center">
          <span class="flex items-center gap-1 font-medium">
            <Lock v-if="m.locked" :size="12" aria-hidden="true" />{{ m.id }}
          </span>
          <span class="text-xs text-ink-2">{{ role(w.roleOf(m)) }} · {{ m.body.device }}</span>
        </span>
        <template v-if="m.locked">
          <span class="text-sm text-ink-2">{{ t('settings.lockedHint') }}</span>
        </template>
        <template v-else-if="w.roleOf(m) === 'consumer'">
          <FieldSelect
            :model-value="str(m.body.category) || 'other'"
            :options="categoryOptions"
            :label="t('wizard.hierarchy.category')"
            @update:model-value="(v: string) => set(m, 'category', v)"
          />
          <FieldSelect
            :model-value="str(m.body.parent) || 'site'"
            :options="parents(m)"
            :label="t('wizard.hierarchy.parent')"
            @update:model-value="(v: string) => set(m, 'parent', v)"
          />
        </template>
        <template v-else-if="w.roleOf(m) === 'pv'">
          <FieldSelect
            :model-value="str(m.body.parent)"
            :options="parents(m)"
            :label="t('wizard.hierarchy.parent')"
            @update:model-value="(v: string) => set(m, 'parent', v)"
          />
          <TextField
            v-if="!m.body.parent"
            :model-value="str(m.body.kwp)"
            type="number"
            :label="t('wizard.hierarchy.kwp')"
            @update:model-value="(v: string) => setNumber(m, 'kwp', v)"
          />
        </template>
        <TextField
          v-else
          :model-value="kWh(m.body.capacity)"
          type="number"
          :label="t('wizard.hierarchy.capacity')"
          @update:model-value="(v: string) => setNumber(m, 'capacity', v)"
        />
      </li>
    </ul>
  </div>
</template>
