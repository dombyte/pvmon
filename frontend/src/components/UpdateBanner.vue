<script setup lang="ts">
import { X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useVersionStore } from '@/stores/version'

const { t } = useI18n()
const version = useVersionStore()
</script>

<template>
  <Transition name="pop">
    <div
      v-if="version.banner"
      role="status"
      class="flex items-center justify-between gap-2 bg-accent pl-4 text-base text-on-accent"
    >
      <span>
        {{ t('update.banner', { version: version.serverVersion }) }}
        <template v-if="version.unavailable">{{ t('update.unavailable') }}</template>
      </span>
      <span class="flex items-center">
        <button type="button" class="min-h-11 rounded-control px-3 font-semibold underline" @click="version.update()">
          {{ t('update.reload') }}
        </button>
        <button
          type="button"
          class="grid size-11 place-items-center rounded-control"
          :aria-label="t('update.dismiss')"
          @click="version.dismiss()"
        >
          <X :size="18" aria-hidden="true" />
        </button>
      </span>
    </div>
  </Transition>
</template>
