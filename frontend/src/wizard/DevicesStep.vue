<script setup lang="ts">
import { Lock, Plus } from '@lucide/vue'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmButton from '@/components/ConfirmButton.vue'
import DeviceEditor from './DeviceEditor.vue'
import { deviceAddress } from './draft'
import { useWizard } from './state'

// Step 2 (spec 11.14): connections and devices. Devices from config.yaml are shown locked.
const { t } = useI18n()
const w = useWizard()
/** The device being edited; '' adds a new one, null shows only the list. */
const open = ref<string | null>(null)
const devices = computed(() => w.draft.value.filter((o) => o.kind === 'device'))
</script>

<template>
  <div class="flex flex-col gap-4">
    <p class="text-sm text-ink-2">{{ t('wizard.devices.intro') }}</p>
    <p v-if="devices.length === 0 && open === null" class="text-sm">
      {{ t('wizard.devices.none') }}
    </p>
    <ul v-if="devices.length" class="flex flex-col">
      <li
        v-for="d in devices"
        :key="d.id"
        class="flex flex-col gap-3 border-t border-line py-3 first:border-t-0"
      >
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span class="font-medium">{{ d.id }}</span>
          <span class="min-w-0 flex-1 truncate text-sm text-ink-2">
            {{
              [d.body.profile, deviceAddress(d, w.draft.value), d.body.unit]
                .filter(Boolean)
                .join(' · ')
            }}
          </span>
          <span v-if="d.locked" class="flex items-center gap-1 text-xs text-ink-2">
            <Lock :size="12" aria-hidden="true" />{{ t('settings.objects.file') }}
          </span>
          <div v-else class="ml-auto flex gap-2">
            <button
              type="button"
              class="min-h-11 rounded-control border border-line px-3 text-sm"
              :aria-expanded="open === d.id"
              @click="open = open === d.id ? null : d.id"
            >
              {{ t('wizard.edit') }}
            </button>
            <ConfirmButton :label="t('settings.delete')" @confirm="w.remove(d.id)" />
          </div>
        </div>
        <DeviceEditor v-if="open === d.id" :device="d.id" @done="open = null" />
      </li>
    </ul>
    <DeviceEditor v-if="open === ''" @done="open = null" />
    <button
      v-else
      type="button"
      class="flex min-h-11 items-center gap-1.5 self-start rounded-control border border-line px-3 text-sm"
      @click="open = ''"
    >
      <Plus :size="16" aria-hidden="true" />{{ t('wizard.devices.add') }}
    </button>
  </div>
</template>
