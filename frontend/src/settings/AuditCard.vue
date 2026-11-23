<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchAudit } from '@/api/client'
import type { AuditEntry } from '@/api/types'
import Card from '@/components/Card.vue'
import { usePolled } from '@/composables/usePolled'
import { useWhen } from './when'

// The audit log of the last 30 days (spec 10), newest first; details on demand. A new
// `version` (a change made on the page) loads it again.
const props = defineProps<{ version: number }>()
const { t } = useI18n()
const when = useWhen()
const { data, error } = usePolled(() => props.version, fetchAudit, 0)
const step = 20
const shown = ref(step)
const entries = computed(() => data.value?.entries.slice(0, shown.value) ?? [])
const more = computed(() => (data.value?.entries.length ?? 0) > shown.value)

const detail = (e: AuditEntry) => (e.detail ? JSON.stringify(e.detail, null, 2) : '')
</script>

<template>
  <Card :title="t('settings.audit.title')">
    <div class="flex flex-col gap-2">
      <p v-if="error" class="text-sm text-fault" role="alert">{{ t(error) }}</p>
      <p v-if="data && data.entries.length === 0" class="text-sm text-ink-2">
        {{ t('settings.none') }}
      </p>
      <ul class="flex flex-col">
        <li
          v-for="e in entries"
          :key="e.id"
          class="flex flex-col gap-1 border-t border-line py-2 first:border-t-0"
        >
          <div class="flex flex-wrap items-baseline gap-x-3">
            <span class="text-sm font-medium">{{ e.action }}</span>
            <span class="text-xs text-ink-2">{{ e.actor ?? '' }}</span>
            <span class="ml-auto text-xs text-ink-2">{{ when(e.at) }}</span>
          </div>
          <details v-if="e.detail" class="text-xs">
            <summary class="cursor-pointer text-ink-2">{{ t('settings.audit.detail') }}</summary>
            <pre class="mt-1 overflow-x-auto rounded-chip bg-well p-2">{{ detail(e) }}</pre>
          </details>
        </li>
      </ul>
      <button
        v-if="more"
        type="button"
        class="min-h-11 self-start rounded-control px-3 text-sm text-ink-2 hover:bg-well"
        @click="shown += step"
      >
        {{ t('settings.audit.more') }}
      </button>
    </div>
  </Card>
</template>
