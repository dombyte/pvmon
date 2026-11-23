<script setup lang="ts">
import { Lock } from '@lucide/vue'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { putSettings } from '@/api/client'
import type { SettingView } from '@/api/types'
import Card from '@/components/Card.vue'
import TextField from '@/components/TextField.vue'
import { useSessionStore } from '@/stores/session'
import { formError, type FormError } from './errors'
import { changesOf, formOf, settingFields, type SettingForm, type SettingKey } from './settings'

// App settings (spec 3): applied without restart; keys from config.yaml or env are locked.
const props = defineProps<{ settings: Record<string, SettingView> }>()
const emit = defineEmits<{ saved: [] }>()
const { t } = useI18n()
const session = useSessionStore()
const form = ref<SettingForm>(formOf(props.settings))
const busy = ref(false)
const saved = ref(false)
const error = ref<FormError | null>(null)
watch(
  () => props.settings,
  (s) => (form.value = formOf(s)),
)

// Keys spelled out for the i18n key check.
const labels: Record<SettingKey, () => string> = {
  'site.name': () => t('settings.general.siteName'),
  'site.interval': () => t('settings.general.interval'),
  'status.offline_after': () => t('settings.general.offlineAfter'),
  'auth.public_read': () => t('settings.general.publicRead'),
  'storage.retention': () => t('settings.general.retention'),
  'storage.backup_interval': () => t('settings.general.backupInterval'),
  'storage.max_backups': () => t('settings.general.maxBackups'),
}
const locked = (k: SettingKey) => props.settings[k]?.locked === true
const hint = (k: SettingKey) => (locked(k) ? t('settings.lockedHint') : undefined)
const text = (k: SettingKey) => ({
  get: () => String(form.value[k]),
  set: (v: string) => (form.value[k] = v),
})

async function save(): Promise<void> {
  const set = changesOf(formOf(props.settings), form.value, props.settings)
  saved.value = false
  error.value = null
  if (Object.keys(set).length === 0) return
  busy.value = true
  try {
    await putSettings(set)
    saved.value = true
    if ('auth.public_read' in set) await session.load()
    emit('saved')
  } catch (err) {
    error.value = formError(err, t)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Card :title="t('settings.general.title')">
    <form class="flex flex-col gap-4" @submit.prevent="save">
      <p class="text-sm text-ink-2">{{ t('settings.general.intro') }}</p>
      <template v-for="f in settingFields" :key="f.key">
        <label v-if="f.kind === 'bool'" class="flex min-h-11 items-center gap-3 text-sm">
          <input
            v-model="form[f.key]"
            type="checkbox"
            class="size-5 accent-accent"
            :disabled="locked(f.key)"
          />
          <span class="flex flex-col">
            <span>{{ labels[f.key]() }}</span>
            <span class="text-xs text-ink-2">
              {{ locked(f.key) ? t('settings.lockedHint') : t('settings.general.publicReadHint') }}
            </span>
          </span>
          <Lock v-if="locked(f.key)" :size="14" class="text-ink-2" aria-hidden="true" />
        </label>
        <TextField
          v-else
          :model-value="text(f.key).get()"
          :type="f.kind === 'number' ? 'number' : 'text'"
          :label="labels[f.key]()"
          :disabled="locked(f.key)"
          :hint="hint(f.key)"
          :error="error?.fields[f.key]"
          @update:model-value="text(f.key).set"
        />
      </template>
      <div class="flex flex-wrap items-center gap-3">
        <button
          type="submit"
          class="min-h-11 rounded-control bg-accent px-4 font-semibold text-on-accent disabled:opacity-60"
          :disabled="busy"
        >
          {{ t('settings.save') }}
        </button>
        <p v-if="saved" class="text-sm text-ink-2" role="status">{{ t('settings.saved') }}</p>
        <p v-if="error?.key" class="text-sm text-fault" role="alert">{{ t(error.key) }}</p>
      </div>
    </form>
  </Card>
</template>
