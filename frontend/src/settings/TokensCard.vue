<script setup lang="ts">
import { Copy } from '@lucide/vue'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { createToken, errorKey, fetchTokens, revokeToken } from '@/api/client'
import type { NewToken } from '@/api/types'
import Card from '@/components/Card.vue'
import ConfirmButton from '@/components/ConfirmButton.vue'
import SelectField from '@/components/select/SelectField.vue'
import TextField from '@/components/TextField.vue'
import { usePolled } from '@/composables/usePolled'
import { useWhen } from './when'

// API tokens (spec 10): admin access for scripts; the secret is shown once after creation.
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const when = useWhen()
const { data, error, reload } = usePolled(true, fetchTokens, 0)

const name = ref('')
const expiry = ref<'0' | '30' | '90' | '365'>('365')
const expiryOptions = computed(() => [
  { value: '30' as const, label: t('settings.tokens.days', { n: 30 }) },
  { value: '90' as const, label: t('settings.tokens.days', { n: 90 }) },
  { value: '365' as const, label: t('settings.tokens.days', { n: 365 }) },
  { value: '0' as const, label: t('settings.tokens.never') },
])
const created = ref<NewToken | null>(null)
const copied = ref(false)
const busy = ref(false)
const actionError = ref<string | null>(null)

async function create(): Promise<void> {
  busy.value = true
  actionError.value = null
  copied.value = false
  try {
    created.value = await createToken(name.value, Number(expiry.value))
    name.value = ''
    emit('changed')
    await reload()
  } catch (err) {
    actionError.value = errorKey(err)
  } finally {
    busy.value = false
  }
}

async function revoke(id: string): Promise<void> {
  actionError.value = null
  try {
    await revokeToken(id)
    if (created.value?.id === id) created.value = null
    emit('changed')
    await reload()
  } catch (err) {
    actionError.value = errorKey(err)
  }
}

async function copy(): Promise<void> {
  if (!created.value) return
  try {
    await navigator.clipboard.writeText(created.value.token)
    copied.value = true
  } catch {
    // No clipboard (plain HTTP): the token stays selectable.
  }
}
</script>

<template>
  <Card :title="t('settings.tokens.title')">
    <div class="flex flex-col gap-4">
      <p class="text-sm text-ink-2">{{ t('settings.tokens.intro') }}</p>
      <p v-if="error" class="text-sm text-fault" role="alert">{{ t(error) }}</p>
      <p v-if="data && data.tokens.length === 0" class="text-sm text-ink-2">
        {{ t('settings.none') }}
      </p>
      <ul v-else-if="data" class="flex flex-col">
        <li
          v-for="k in data.tokens"
          :key="k.id"
          class="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-line py-2 first:border-t-0"
        >
          <span class="font-medium">{{ k.name }}</span>
          <span class="order-last w-full text-xs text-ink-2 min-[768px]:order-none min-[768px]:w-auto min-[768px]:min-w-0 min-[768px]:flex-1">
            {{
              k.expires_at
                ? t('settings.tokens.expires', { when: when(k.expires_at) })
                : t('settings.tokens.noExpiry')
            }}
            · {{ t('settings.tokens.lastUsed', { when: when(k.last_used) }) }}
          </span>
          <ConfirmButton
            class="ml-auto"
            :label="t('settings.tokens.revoke')" @confirm="revoke(k.id)"
          />
        </li>
      </ul>
      <div v-if="created" class="flex flex-col gap-2 rounded-control bg-well p-3" role="status">
        <p class="text-sm font-semibold">{{ t('settings.tokens.once') }}</p>
        <div class="flex items-center gap-2">
          <code class="min-w-0 flex-1 break-all text-sm select-all">{{ created.token }}</code>
          <button
            type="button"
            class="grid size-11 shrink-0 place-items-center rounded-control text-ink-2 hover:bg-well"
            :aria-label="t('settings.tokens.copy')"
            :title="t('settings.tokens.copy')"
            @click="copy"
          >
            <Copy :size="16" aria-hidden="true" />
          </button>
        </div>
        <p v-if="copied" class="text-xs text-ink-2">{{ t('settings.tokens.copied') }}</p>
      </div>
      <form class="flex flex-col gap-3 border-t border-line pt-4" @submit.prevent="create">
        <h3 class="text-sm font-semibold">{{ t('settings.tokens.add') }}</h3>
        <div class="flex flex-wrap items-end gap-3">
          <div class="min-w-48 flex-1">
            <TextField v-model="name" :label="t('settings.tokens.name')" required />
          </div>
          <div class="flex flex-col gap-1 text-sm">
            <span class="text-ink-2" aria-hidden="true">{{ t('settings.tokens.expiry') }}</span>
            <SelectField
              v-model="expiry"
              :options="expiryOptions"
              :label="t('settings.tokens.expiry')"
            />
          </div>
          <button
            type="submit"
            class="min-h-11 rounded-control bg-accent px-4 font-semibold text-on-accent disabled:opacity-60"
            :disabled="busy"
          >
            {{ t('settings.tokens.create') }}
          </button>
        </div>
        <p v-if="actionError" class="text-sm text-fault" role="alert">{{ t(actionError) }}</p>
      </form>
    </div>
  </Card>
</template>
