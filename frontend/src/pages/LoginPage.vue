<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AuthCard from '@/settings/AuthCard.vue'
import TextField from '@/components/TextField.vue'
import { errorKey } from '@/api/client'
import { useSessionStore } from '@/stores/session'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const name = ref('')
const password = ref('')
const busy = ref(false)
const error = ref<string | null>(null)

/** Only same-app paths are followed after login. */
function next(): string {
  const n = route.query.next
  return typeof n === 'string' && n.startsWith('/') && !n.startsWith('//') ? n : '/'
}

async function submit(): Promise<void> {
  busy.value = true
  error.value = null
  try {
    await session.login(name.value, password.value)
    await router.replace(next())
  } catch (err) {
    error.value = errorKey(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthCard :title="t('login.title')">
    <form class="flex flex-col gap-4" @submit.prevent="submit">
      <TextField v-model="name" :label="t('login.name')" autocomplete="username" required />
      <TextField
        v-model="password"
        type="password"
        :label="t('login.password')"
        autocomplete="current-password"
        required
      />
      <p v-if="error" class="text-sm text-fault" role="alert">
        {{ error === 'errors.unauthorized' ? t('login.failed') : t(error) }}
      </p>
      <button
        type="submit"
        class="min-h-11 rounded-control bg-accent px-3 font-semibold text-on-accent disabled:opacity-60"
        :disabled="busy"
      >
        {{ t('login.submit') }}
      </button>
      <RouterLink
        v-if="session.me?.public_read"
        to="/"
        class="text-center text-sm text-ink-2 underline"
      >
        {{ t('login.back') }}
      </RouterLink>
    </form>
  </AuthCard>
</template>
