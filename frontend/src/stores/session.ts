import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { fetchMe, login as apiLogin, logout as apiLogout, setup as apiSetup } from '@/api/client'
import type { SetupRequest } from '@/api/client'
import type { Me, UserInfo } from '@/api/types'
import { isLang } from '@/i18n'
import { usePreferencesStore } from './preferences'

/**
 * Who is logged in and whether the UI may show data at all (spec 9.1, 11.14). `ready` is
 * false until the first answer; a failure keeps the last known state.
 */
export const useSessionStore = defineStore('session', () => {
  const me = ref<Me | null>(null)
  const ready = computed(() => me.value !== null)
  const user = computed(() => me.value?.user ?? null)
  const setupRequired = computed(() => me.value?.setup_required ?? false)
  /** Data pages are readable: logged in, or auth.public_read. */
  const canRead = computed(() => !!me.value && (me.value.public_read || !!me.value.user))
  let pending: Promise<void> | null = null

  /** Loads /auth/me once; later callers share the request. */
  function load(): Promise<void> {
    pending ??= fetchMe()
      .then((m) => {
        me.value = m
      })
      .finally(() => {
        pending = null
      })
    return pending
  }

  async function login(name: string, password: string): Promise<UserInfo> {
    const r = await apiLogin(name, password)
    // The language of the user profile wins over the browser's (spec 11.10).
    if (isLang(r.user.lang)) usePreferencesStore().language = r.user.lang
    await load()
    return r.user
  }

  async function setup(req: SetupRequest): Promise<UserInfo> {
    const r = await apiSetup(req)
    await load()
    return r.user
  }

  async function logout(): Promise<void> {
    await apiLogout()
    await load()
  }

  return { me, ready, user, setupRequired, canRead, load, login, setup, logout }
})
