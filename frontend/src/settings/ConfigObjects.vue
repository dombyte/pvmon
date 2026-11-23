<script setup lang="ts">
import { Download, Lock, Wand } from '@lucide/vue'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { downloadConfig, errorKey } from '@/api/client'
import type { ConfigObject } from '@/api/types'
import Card from '@/components/Card.vue'
import { saveBlob } from '@/composables/saveBlob'
import { useWhen } from './when'

// Connections, devices and meters with their origin; the setup wizard edits them.
const props = defineProps<{ objects: ConfigObject[] }>()
const { t } = useI18n()
const when = useWhen()
const error = ref<string | null>(null)

const groups = computed(() =>
  (['connection', 'device', 'meter'] as const).map((kind) => ({
    kind,
    items: props.objects.filter((o) => o.kind === kind),
  })),
)
// Keys spelled out for the i18n key check.
const titles = {
  connection: () => t('settings.objects.connections'),
  device: () => t('settings.objects.devices'),
  meter: () => t('settings.objects.meters'),
}

/** One line about the object: what it is and where it points. */
function summary(o: ConfigObject): string {
  const b = o.body
  const parts = [b.address, b.connection, b.profile, b.device, b.source, b.category, b.label]
  return parts.filter((p) => typeof p === 'string' || typeof p === 'number').join(' · ')
}

async function exportConfig(): Promise<void> {
  error.value = null
  try {
    saveBlob(await downloadConfig(), 'config.yaml')
  } catch (err) {
    error.value = errorKey(err)
  }
}
</script>

<template>
  <Card :title="t('settings.objects.title')">
    <div class="flex flex-col gap-4">
      <p class="text-sm text-ink-2">{{ t('settings.objects.intro') }}</p>
      <section v-for="g in groups" :key="g.kind" class="flex flex-col gap-1">
        <h3 class="text-sm font-semibold">{{ titles[g.kind]() }}</h3>
        <p v-if="g.items.length === 0" class="text-sm text-ink-2">{{ t('settings.none') }}</p>
        <ul v-else class="flex flex-col">
          <li
            v-for="o in g.items"
            :key="o.id"
            class="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 border-t border-line py-2 first:border-t-0"
          >
            <span class="font-medium">{{ o.id }}</span>
            <span class="min-w-0 flex-1 truncate text-sm text-ink-2">{{ summary(o) }}</span>
            <span v-if="o.locked" class="flex items-center gap-1 text-xs text-ink-2">
              <Lock :size="12" aria-hidden="true" />{{ t('settings.objects.file') }}
            </span>
            <span v-else class="text-xs text-ink-2">
              {{ t('settings.objects.ui', { when: when(o.updated_at) }) }}
            </span>
            <span v-if="o.unreadable?.length" class="w-full text-xs text-fault">
              {{ t('settings.objects.unreadable', { paths: o.unreadable.join(', ') }) }}
            </span>
          </li>
        </ul>
      </section>
      <div class="flex flex-wrap items-center gap-3">
        <RouterLink
          :to="{ name: 'wizard' }"
          class="flex min-h-11 items-center gap-1.5 rounded-control bg-accent px-3 text-sm font-medium text-on-accent"
        >
          <Wand :size="16" aria-hidden="true" />
          {{ t('settings.objects.wizard') }}
        </RouterLink>
        <button
          type="button"
          class="flex min-h-11 items-center gap-1.5 rounded-control border border-line px-3 text-sm"
          @click="exportConfig"
        >
          <Download :size="16" aria-hidden="true" />
          {{ t('settings.objects.export') }}
        </button>
        <p v-if="error" class="text-sm text-fault" role="alert">{{ t(error) }}</p>
      </div>
    </div>
  </Card>
</template>
