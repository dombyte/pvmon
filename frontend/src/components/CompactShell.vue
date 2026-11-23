<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { compactNav } from '@/layout/nav'
import NavLink from './NavLink.vue'

const { t } = useI18n()
</script>

<template>
  <div class="flex h-full flex-col">
    <!-- No top bar: every page has its title; theme and language live under "More". -->
    <main class="compact-main min-h-0 flex-1 overflow-y-auto">
      <slot />
    </main>
    <nav
      :aria-label="t('nav.mobile')"
      class="glass compact-nav fixed inset-x-0 bottom-0 z-10 flex"
    >
      <NavLink v-for="item in compactNav" :key="item.to" :item="item" stacked />
    </nav>
  </div>
</template>
