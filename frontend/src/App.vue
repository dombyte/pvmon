<script setup lang="ts">
import CompactShell from '@/components/CompactShell.vue'
import DesktopShell from '@/components/DesktopShell.vue'
import LoadingRing from '@/components/LoadingRing.vue'
import OfflineBanner from '@/components/OfflineBanner.vue'
import UpdateBanner from '@/components/UpdateBanner.vue'
import { useLayout } from '@/layout/mode'
import { useSiteStore } from '@/stores/site'
import { useRoute } from 'vue-router'

const { mode } = useLayout()
const current = useRoute()
const site = useSiteStore()
</script>

<template>
  <!-- The notch inset sits above banner and pages alike (viewport-fit=cover). -->
  <div class="safe-top flex h-full flex-col">
    <UpdateBanner />
    <OfflineBanner />
    <div class="min-h-0 flex-1">
      <!-- Login and setup stand alone: no navigation to pages that are not readable yet. -->
      <RouterView v-if="current.meta.bare" class="h-full overflow-y-auto" />
      <component
        :is="mode === 'compact' ? CompactShell : DesktopShell" v-else
      >
        <!-- The ring stands in for the page until the first site model or a load error. -->
        <LoadingRing v-if="!site.model && !site.error" />
        <RouterView v-else v-slot="{ Component, route }">
          <Transition name="page">
            <component :is="Component" :key="route.path" />
          </Transition>
        </RouterView>
      </component>
    </div>
  </div>
</template>
