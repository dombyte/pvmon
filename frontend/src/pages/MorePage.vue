<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { watchEffect } from 'vue'
import Card from '@/components/Card.vue'
import NavLink from '@/components/NavLink.vue'
import Page from '@/components/Page.vue'
import StatusCard from '@/components/StatusCard.vue'
import { useLayout } from '@/layout/mode'
import { moreNav } from '@/layout/nav'

const { t } = useI18n()
const router = useRouter()
const { mode } = useLayout()

// The menu exists only in the compact layout; desktop has the sidebar.
watchEffect(() => {
  if (mode.value === 'desktop') void router.replace('/')
})
</script>

<template>
  <Page :title="t('nav.more')">
    <Card>
      <nav :aria-label="t('nav.more')" class="flex flex-col gap-1">
        <NavLink v-for="item in moreNav" :key="item.to" :item="item" />
      </nav>
    </Card>
    <Card>
      <StatusCard bare />
    </Card>
  </Page>
</template>
