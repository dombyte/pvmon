<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { probeDevice } from '@/api/client'
import type { ProbeResult } from '@/api/types'
import FieldSelect from './FieldSelect.vue'
import TextField from '@/components/TextField.vue'
import { formError, type FormError } from '@/settings/errors'
import {
  emptyDeviceForm,
  formOf,
  freeID,
  isValidID,
  objectsOf,
  probeChanges,
  type DeviceForm,
  type Transport,
} from './draft'
import ProbeView from './ProbeView.vue'
import ProblemList from './ProblemList.vue'
import { useWizard } from './state'

// One device (spec 11.14 step 2): address, unit ID, profile; "Test" reads it once before
// it is added. `device` is the ID of a device of the draft to edit, empty for a new one.
const props = defineProps<{ device?: string }>()
const emit = defineEmits<{ done: [] }>()
const { t } = useI18n()
const w = useWizard()

const editing = computed(() =>
  w.draft.value.find((o) => o.kind === 'device' && o.id === props.device),
)
const form = ref<DeviceForm>(
  editing.value ? formOf(editing.value, w.draft.value) : emptyDeviceForm(),
)
const localFile = '__file__'
const builtin = computed(() => w.profiles.value.some((p) => p.id === form.value.profile))
const profileChoice = ref(builtin.value || !form.value.profile ? form.value.profile : localFile)
const result = ref<ProbeResult | null>(null)
const error = ref<FormError | null>(null)
const busy = ref(false)
/** The unit ID being tried by the search, for the status line. */
const trying = ref<number | null>(null)

const transports = computed<{ value: Transport; label: string }[]>(() => [
  { value: 'tcp', label: t('wizard.device.tcp') },
  { value: 'rtu', label: t('wizard.device.rtu') },
  { value: 'http', label: t('wizard.device.http') },
])
const profileOptions = computed(() => {
  const kind = form.value.transport === 'http' ? 'http' : 'modbus'
  const list = w.profiles.value
    .filter((p) => p.transport === kind)
    .map((p) => ({
      value: p.id,
      label: `${p.vendor} ${p.models.join(', ')} (${p.id})`,
    }))
  return [
    { value: '', label: t('wizard.device.chooseProfile') },
    ...list,
    { value: localFile, label: t('wizard.device.localFile') },
  ]
})
const parities = [
  { value: 'N', label: 'N' },
  { value: 'E', label: 'E' },
  { value: 'O', label: 'O' },
]
const stopBits = [
  { value: '1', label: '1' },
  { value: '2', label: '2' },
]

watch(profileChoice, (v) => {
  form.value.profile = v === localFile ? '' : v
  if (!editing.value && v && v !== localFile) {
    form.value.id = freeID(v.split('/').pop() ?? '', 'device', w.draft.value)
  }
})
watch(
  () => form.value.transport,
  () => {
    if (!profileOptions.value.some((o) => o.value === profileChoice.value)) profileChoice.value = ''
  },
)

const idError = computed(() => {
  const id = form.value.id
  if (!id) return undefined
  if (!isValidID(id)) return t('wizard.device.badID')
  const taken = w.draft.value.some((o) => o.kind === 'device' && o.id === id && id !== props.device)
  return taken ? t('wizard.device.takenID') : undefined
})
const ready = computed(() => form.value.id !== '' && !idError.value && form.value.profile !== '')
const isModbus = computed(() => form.value.transport !== 'http')

async function read(): Promise<ProbeResult | null> {
  error.value = null
  try {
    const objs = objectsOf(form.value, w.draft.value)
    return await probeDevice(probeChanges(objs), form.value.id)
  } catch (err) {
    error.value = formError(err, t)
    return null
  }
}

async function test(): Promise<void> {
  busy.value = true
  result.value = await read()
  busy.value = false
}

/** Common unit IDs first; stops at the first one that answers. */
const unitCandidates = [1, 2, 3, 4, 5, 10, 100, 247]
async function scan(): Promise<void> {
  busy.value = true
  const before = form.value.unit
  for (const u of unitCandidates) {
    trying.value = u
    form.value.unit = String(u)
    const res = await read()
    if (!res) break
    result.value = res
    if (!res.error) {
      trying.value = null
      busy.value = false
      return
    }
  }
  if (result.value?.error) form.value.unit = before
  trying.value = null
  busy.value = false
}

function apply(): void {
  w.apply(
    objectsOf(form.value, w.draft.value),
    result.value?.error ? undefined : (result.value ?? undefined),
  )
  emit('done')
}
</script>

<template>
  <form class="flex flex-col gap-3" @submit.prevent="apply">
    <div class="grid gap-3 min-[640px]:grid-cols-2">
      <FieldSelect
        v-model="form.transport"
        :options="transports"
        :label="t('wizard.device.transport')"
      />
      <FieldSelect
        v-model="profileChoice"
        :options="profileOptions"
        :label="t('wizard.device.profile')"
      />
      <TextField
        v-if="profileChoice === localFile"
        v-model="form.profile"
        :label="t('wizard.device.profilePath')"
        :hint="t('wizard.device.profilePathHint')"
        class="min-[640px]:col-span-2"
      />
      <template v-if="form.transport === 'tcp'">
        <TextField v-model="form.host" :label="t('wizard.device.host')" required />
        <TextField v-model="form.port" type="number" :label="t('wizard.device.port')" />
      </template>
      <template v-else-if="form.transport === 'rtu'">
        <TextField v-model="form.serial" :label="t('wizard.device.serial')" required />
        <TextField v-model="form.speed" type="number" :label="t('wizard.device.speed')" />
        <FieldSelect v-model="form.parity" :options="parities" :label="t('wizard.device.parity')" />
        <FieldSelect
          v-model="form.stopBits"
          :options="stopBits"
          :label="t('wizard.device.stopBits')"
        />
      </template>
      <template v-else>
        <TextField
          v-model="form.url"
          :label="t('wizard.device.url')"
          required
          class="min-[640px]:col-span-2"
        />
        <TextField v-model="form.user" :label="t('wizard.device.user')" autocomplete="off" />
        <TextField
          v-model="form.password"
          type="password"
          :label="t('wizard.device.password')"
          autocomplete="new-password"
        />
      </template>
      <TextField
        v-if="isModbus"
        v-model="form.unit"
        type="number"
        :label="t('wizard.device.unit')"
      />
      <TextField
        v-model="form.id"
        :label="t('wizard.device.id')"
        :disabled="!!editing"
        :error="idError"
        :hint="t('wizard.device.idHint')"
        required
      />
      <TextField
        v-model="form.interval"
        :label="t('wizard.device.interval')"
        :hint="t('wizard.device.intervalHint')"
      />
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="min-h-11 rounded-control border border-line px-3 text-sm disabled:opacity-60"
        :disabled="!ready || busy"
        @click="test"
      >
        {{ t('wizard.probe.test') }}
      </button>
      <button
        v-if="isModbus"
        type="button"
        class="min-h-11 rounded-control border border-line px-3 text-sm disabled:opacity-60"
        :disabled="!ready || busy"
        @click="scan"
      >
        {{ t('wizard.probe.scan') }}
      </button>
      <button
        type="submit"
        class="min-h-11 rounded-control bg-accent px-4 text-sm font-medium text-on-accent disabled:opacity-60"
        :disabled="!ready || busy"
      >
        {{ editing ? t('wizard.device.update') : t('wizard.device.add') }}
      </button>
      <button type="button" class="min-h-11 px-3 text-sm text-ink-2" @click="emit('done')">
        {{ t('wizard.cancel') }}
      </button>
      <p v-if="busy" class="text-sm text-ink-2" role="status">
        {{
          trying !== null ? t('wizard.probe.trying', { unit: trying }) : t('wizard.probe.reading')
        }}
      </p>
    </div>
    <ProblemList v-if="error" :error="error" />
    <ProbeView v-if="result" :result="result" />
    <p v-if="result && !result.error && !editing" class="text-sm text-ink-2">
      {{ t('wizard.device.addHint') }}
    </p>
  </form>
</template>
