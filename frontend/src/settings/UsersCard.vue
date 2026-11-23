<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { addUser, deleteUser, errorKey, fetchUsers, setPassword } from '@/api/client'
import Card from '@/components/Card.vue'
import ConfirmButton from '@/components/ConfirmButton.vue'
import TextField from '@/components/TextField.vue'
import { usePolled } from '@/composables/usePolled'
import { usePreferencesStore } from '@/stores/preferences'
import { useSessionStore } from '@/stores/session'
import { formError, type FormError } from './errors'
import { useWhen } from './when'

// Users (spec 10): every user is an admin; the last one cannot be deleted.
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const when = useWhen()
const session = useSessionStore()
const prefs = usePreferencesStore()
const { data, error, reload } = usePolled(true, fetchUsers, 0)

const name = ref('')
const password = ref('')
const adding = ref(false)
const addError = ref<FormError | null>(null)
/** The user whose password is being changed. */
const editing = ref<number | null>(null)
const newPassword = ref('')
const pwError = ref<FormError | null>(null)
const actionError = ref<string | null>(null)

async function add(): Promise<void> {
  adding.value = true
  addError.value = null
  try {
    await addUser(name.value, password.value, prefs.language)
    name.value = ''
    password.value = ''
    emit('changed')
    await reload()
  } catch (err) {
    addError.value = formError(err, t)
  } finally {
    adding.value = false
  }
}

async function changePassword(id: number): Promise<void> {
  pwError.value = null
  try {
    await setPassword(id, newPassword.value)
    editing.value = null
    newPassword.value = ''
    emit('changed')
    // Changing one's own password ends one's own sessions too.
    if (id === session.user?.id) await session.load()
  } catch (err) {
    pwError.value = formError(err, t)
  }
}

async function remove(id: number): Promise<void> {
  actionError.value = null
  try {
    await deleteUser(id)
    emit('changed')
    await reload()
    if (id === session.user?.id) await session.load()
  } catch (err) {
    actionError.value = errorKey(err)
  }
}

function edit(id: number): void {
  editing.value = editing.value === id ? null : id
  newPassword.value = ''
  pwError.value = null
}
</script>

<template>
  <Card :title="t('settings.users.title')">
    <div class="flex flex-col gap-4">
      <p v-if="error" class="text-sm text-fault" role="alert">{{ t(error) }}</p>
      <ul v-if="data" class="flex flex-col">
        <li
          v-for="u in data.users"
          :key="u.id"
          class="flex flex-col gap-2 border-t border-line py-2 first:border-t-0"
        >
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span class="font-medium">{{ u.name }}</span>
            <span v-if="u.id === session.user?.id" class="text-xs text-ink-2">
              {{ t('settings.users.you') }}
            </span>
            <span class="order-last w-full text-xs text-ink-2 min-[768px]:order-none min-[768px]:w-auto min-[768px]:min-w-0 min-[768px]:flex-1">
              {{ t('settings.users.lastLogin', { when: when(u.last_login) }) }}
            </span>
            <button
              type="button"
              class="ml-auto min-h-11 rounded-control px-3 text-sm text-ink-2 hover:bg-well"
              :aria-expanded="editing === u.id"
              @click="edit(u.id)"
            >
              {{ t('settings.users.password') }}
            </button>
            <ConfirmButton :label="t('settings.delete')" @confirm="remove(u.id)" />
          </div>
          <form
            v-if="editing === u.id"
            class="flex flex-wrap items-end gap-3"
            @submit.prevent="changePassword(u.id)"
          >
            <div class="min-w-48 flex-1">
              <TextField
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                :label="t('settings.users.newPassword', { name: u.name })"
                :error="pwError?.fields.password"
                required
              />
            </div>
            <button
              type="submit"
              class="min-h-11 rounded-control bg-accent px-4 font-semibold text-on-accent"
            >
              {{ t('settings.save') }}
            </button>
            <p v-if="pwError?.key && !pwError.fields.password" class="text-sm text-fault" role="alert">
              {{ t(pwError.key) }}
            </p>
          </form>
        </li>
      </ul>
      <p v-if="actionError" class="text-sm text-fault" role="alert">
        {{ actionError === 'errors.conflict' ? t('settings.users.last') : t(actionError) }}
      </p>
      <form class="flex flex-col gap-3 border-t border-line pt-4" @submit.prevent="add">
        <h3 class="text-sm font-semibold">{{ t('settings.users.add') }}</h3>
        <div class="grid gap-3 md:grid-cols-2">
          <TextField
            v-model="name"
            :label="t('login.name')"
            autocomplete="off"
            :error="addError?.fields.name"
            required
          />
          <TextField
            v-model="password"
            type="password"
            autocomplete="new-password"
            :label="t('login.password')"
            :hint="t('password.policy')"
            :error="addError?.fields.password"
            required
          />
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <button
            type="submit"
            class="min-h-11 rounded-control bg-accent px-4 font-semibold text-on-accent disabled:opacity-60"
            :disabled="adding"
          >
            {{ t('settings.users.addSubmit') }}
          </button>
          <p
            v-if="addError?.key && !addError.fields.password"
            class="text-sm text-fault"
            role="alert"
          >
            {{ addError.key === 'errors.conflict' ? t('settings.users.taken') : t(addError.key) }}
          </p>
        </div>
      </form>
    </div>
  </Card>
</template>
