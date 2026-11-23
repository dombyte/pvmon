<script setup lang="ts">
import { LogIn, LogOut, RefreshCw, UserRound } from '@lucide/vue'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSiteStore } from '@/stores/site'
import { builtVersion, useVersionStore, type CheckResult } from '@/stores/version'
import { useLogout } from '@/settings/useLogout'
import { useSessionStore } from '@/stores/session'
import LanguageSelect from './LanguageSelect.vue'
import ThemeToggle from './ThemeToggle.vue'

/** `bare` drops the inner well when the card already sits in a card (More page). */
defineProps<{ bare?: boolean }>()
const { t } = useI18n()
const site = useSiteStore()
const version = useVersionStore()
const session = useSessionStore()
const logout = useLogout()
const result = ref<CheckResult | null>(null)
async function checkNow(): Promise<void> {
  result.value = await version.checkNow()
}
// Keys spelled out for the i18n key check.
const resultText = (r: Exclude<CheckResult, 'available'>) =>
  ({ current: t('update.current'), dev: t('update.dev'), failed: t('update.failed') })[r]

</script>

<template>
  <div class="flex flex-col gap-2" :class="bare ? '' : 'rounded-control bg-well p-3'">
    <p class="text-sm">
      <template v-if="site.devicesTotal > 0">
        {{ t('sidebar.devicesOnline', { online: site.devicesOnline, total: site.devicesTotal }) }}
      </template>
      <template v-else>{{ t('sidebar.noDevices') }}</template>
    </p>
    <div class="flex items-center justify-between gap-2">
      <p class="text-xs text-ink-2">{{ t('sidebar.version', { version: builtVersion }) }}</p>
      <button
        type="button"
        class="grid size-11 place-items-center rounded-control text-ink-2 hover:bg-well"
        :title="t('update.check')"
        :aria-label="t('update.check')"
        :disabled="version.checking"
        @click="checkNow"
      >
        <RefreshCw :size="16" :class="version.checking ? 'animate-spin' : ''" aria-hidden="true" />
      </button>
    </div>
    <p v-if="result" class="-mt-2 text-xs" role="status">
      <button
        v-if="result === 'available'"
        type="button"
        class="min-h-6 font-semibold text-accent underline"
        @click="version.update()"
      >
        {{ t('update.available', { version: version.serverVersion }) }}
      </button>
      <span v-else class="text-ink-2">{{ resultText(result) }}</span>
    </p>
    <div v-if="session.user" class="flex items-center justify-between gap-2">
      <p
        class="flex min-w-0 items-center gap-1.5 text-xs text-ink-2"
        :title="t('login.as', { name: session.user.name })"
      >
        <UserRound :size="14" class="shrink-0" aria-hidden="true" />
        <span class="truncate">{{ session.user.name }}</span>
        <span class="sr-only">{{ t('login.as', { name: session.user.name }) }}</span>
      </p>
      <button
        type="button"
        class="flex min-h-11 items-center gap-1.5 rounded-control px-3 text-sm text-ink-2 hover:bg-well"
        @click="logout"
      >
        <LogOut :size="16" aria-hidden="true" />
        {{ t('login.logout') }}
      </button>
    </div>
    <RouterLink
      v-else-if="session.ready && !session.setupRequired"
      to="/login"
      class="flex min-h-11 items-center gap-1.5 rounded-control px-3 text-sm text-ink-2 hover:bg-well"
    >
      <LogIn :size="16" aria-hidden="true" />
      {{ t('login.title') }}
    </RouterLink>
    <div class="flex items-center justify-between gap-2">
      <LanguageSelect />
      <ThemeToggle />
    </div>
  </div>
</template>
