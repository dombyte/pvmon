<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <!-- The ring of the logo (the geometry of the large app icon). -->
  <div role="status" class="flex h-full items-center justify-center">
    <svg class="size-16 animate-swing motion-reduce:animate-none" viewBox="0 0 32 32" aria-hidden="true">
      <g fill="none" stroke-width="5" stroke-linecap="round" transform="rotate(-75 16 16)">
        <circle class="stroke-pv" cx="16" cy="16" r="12" pathLength="100" stroke-dasharray="36 64" />
        <circle class="stroke-battery" cx="16" cy="16" r="12" pathLength="100" stroke-dasharray="20 80" stroke-dashoffset="-46" />
        <circle class="stroke-grid" cx="16" cy="16" r="12" pathLength="100" stroke-dasharray="14 86" stroke-dashoffset="-76" />
      </g>
    </svg>
    <span class="sr-only">{{ t('common.loading') }}</span>
  </div>
</template>
