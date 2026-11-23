<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AuthCard from '@/settings/AuthCard.vue'
import TextField from '@/components/TextField.vue'
import { formError, type FormError } from '@/settings/errors'
import { usePreferencesStore } from '@/stores/preferences'
import { useSessionStore } from '@/stores/session'
import { useSiteStore } from '@/stores/site'

// First start (spec 11.14 step 1): setup code from the log, first admin, language.
const { t } = useI18n()
const router = useRouter()
const session = useSessionStore()
const prefs = usePreferencesStore()
const site = useSiteStore()
const code = ref('')
const name = ref('')
const password = ref('')
const repeat = ref('')
const busy = ref(false)
const error = ref<FormError | null>(null)

async function submit(): Promise<void> {
  if (password.value !== repeat.value) {
    error.value = { key: '', fields: { repeat: t('password.mismatch') } }
    return
  }
  busy.value = true
  error.value = null
  try {
    await session.setup({
      code: code.value.trim(),
      name: name.value,
      password: password.value,
      lang: prefs.language,
    })
    // A site without devices continues with the setup wizard (spec 11.14).
    await site.load()
    await router.replace(site.model?.meters.length === 0 ? '/wizard' : '/settings')
  } catch (err) {
    const e = formError(err, t)
    if (Object.keys(e.fields).length > 0) e.key = ''
    error.value = e
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthCard :title="t('setup.title')">
    <p class="text-sm text-ink-2">{{ t('setup.intro') }}</p>
    <form class="flex flex-col gap-4" @submit.prevent="submit">
      <TextField
        v-model="code"
        :label="t('setup.code')"
        autocomplete="one-time-code"
        :hint="t('setup.codeHint')"
        required
      />
      <TextField v-model="name" :label="t('login.name')" autocomplete="username" required />
      <TextField
        v-model="password"
        type="password"
        :label="t('login.password')"
        autocomplete="new-password"
        :hint="t('password.policy')"
        :error="error?.fields.password"
        required
      />
      <TextField
        v-model="repeat"
        type="password"
        :label="t('password.repeat')"
        autocomplete="new-password"
        :error="error?.fields.repeat"
        required
      />
      <p v-if="error?.key" class="text-sm text-fault" role="alert">
        {{ error.key === 'errors.forbidden' ? t('setup.badCode') : t(error.key) }}
      </p>
      <button
        type="submit"
        class="min-h-11 rounded-control bg-accent px-3 font-semibold text-on-accent disabled:opacity-60"
        :disabled="busy"
      >
        {{ t('setup.submit') }}
      </button>
    </form>
  </AuthCard>
</template>
