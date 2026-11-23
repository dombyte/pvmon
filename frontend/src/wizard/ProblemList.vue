<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { FormError } from '@/settings/errors'

// A rejected request: the message and every problem with its configuration path.
defineProps<{ error: FormError }>()
const { t } = useI18n()
</script>

<template>
  <div class="flex flex-col gap-1 text-sm text-fault" role="alert">
    <p>{{ t(error.key) }}</p>
    <ul v-if="Object.keys(error.fields).length" class="list-disc pl-5">
      <li v-for="(msg, path) in error.fields" :key="path">
        <code>{{ path }}</code>: {{ msg }}
      </li>
    </ul>
  </div>
</template>
