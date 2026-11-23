<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'

const { t } = useI18n()
const route = useRoute()
const title = computed(() => t(String(route.meta.titleKey)))
</script>

<template>
  <Page :title="title">
    <Card>
      <p class="text-ink-2">{{ t('pages.placeholder') }}</p>
    </Card>
  </Page>
</template>
