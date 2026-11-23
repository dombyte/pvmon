<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LanguageSelect from '@/components/LanguageSelect.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'

// The frame of login and setup: one centred card outside the app shell.
defineProps<{ title: string }>()
const { t } = useI18n()
</script>

<template>
  <main class="flex min-h-full items-center justify-center p-4">
    <div class="flex w-full max-w-sm flex-col gap-4">
      <p class="text-center text-lg font-semibold">{{ t('app.name') }}</p>
      <section class="glass flex flex-col gap-4 rounded-card p-5">
        <h1 class="text-xl font-semibold">{{ title }}</h1>
        <slot />
      </section>
      <div class="flex items-center justify-between gap-2">
        <LanguageSelect />
        <ThemeToggle />
      </div>
    </div>
  </main>
</template>
