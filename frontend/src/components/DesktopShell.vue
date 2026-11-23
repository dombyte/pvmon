<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { desktopNav } from '@/layout/nav'
import NavLink from './NavLink.vue'
import StatusCard from './StatusCard.vue'

const { t } = useI18n()
</script>

<template>
  <div class="flex h-full">
    <aside class="h-full w-62 shrink-0 p-3">
      <div class="glass flex h-full flex-col justify-between gap-4 rounded-card p-2">
        <nav :aria-label="t('nav.main')" class="flex flex-col gap-1">
          <p class="px-3 py-2 text-lg font-semibold">{{ t('app.name') }}</p>
          <NavLink v-for="item in desktopNav" :key="item.to" :item="item" />
        </nav>
        <StatusCard />
      </div>
    </aside>
    <main class="min-w-0 flex-1 overflow-y-auto">
      <slot />
    </main>
  </div>
</template>
