<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchConfig } from '@/api/client'
import Page from '@/components/Page.vue'
import { usePolled } from '@/composables/usePolled'
import AuditCard from '@/settings/AuditCard.vue'
import ConfigObjects from '@/settings/ConfigObjects.vue'
import GeneralSettings from '@/settings/GeneralSettings.vue'
import TokensCard from '@/settings/TokensCard.vue'
import UsersCard from '@/settings/UsersCard.vue'

// Settings (spec 11.3, admin only): app settings, configuration, users, tokens, audit log.
const { t } = useI18n()
const { data, error, reload } = usePolled(true, fetchConfig, 0)
/** Counts changes made here, so the audit log shows them. */
const changes = ref(0)
function saved(): void {
  changes.value++
  void reload()
}
</script>

<template>
  <Page :title="t('nav.settings')">
    <p v-if="error" class="text-sm text-fault" role="alert">{{ t(error) }}</p>
    <div class="grid items-start gap-4 min-[1024px]:grid-cols-2">
      <GeneralSettings v-if="data" :settings="data.settings" @saved="saved" />
      <ConfigObjects v-if="data" :objects="data.objects" />
      <UsersCard @changed="changes++" />
      <TokensCard @changed="changes++" />
    </div>
    <AuditCard :version="changes" />
  </Page>
</template>
