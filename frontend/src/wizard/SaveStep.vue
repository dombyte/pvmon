<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { putObjects } from '@/api/client'
import { formError, type FormError } from '@/settings/errors'
import { useSiteStore } from '@/stores/site'
import { changesOf } from './draft'
import ProblemList from './ProblemList.vue'
import { useWizard } from './state'

// Step 6 (spec 11.14): everything is saved as one revision and applied without restart;
// the server validates the whole site and lists every problem with its path.
const emit = defineEmits<{ saved: [] }>()
const { t } = useI18n()
const w = useWizard()
const site = useSiteStore()
const busy = ref(false)
const error = ref<FormError | null>(null)

const changes = computed(() => changesOf(w.orig.value, w.draft.value))
const isNew = (c: { kind: string; id: string }) =>
  !w.orig.value.some((o) => o.kind === c.kind && o.id === c.id)

// Keys spelled out for the i18n key check.
const kinds: Record<string, () => string> = {
  connection: () => t('wizard.save.connection'),
  device: () => t('wizard.save.device'),
  meter: () => t('wizard.save.meter'),
}

function what(c: { kind: string; id: string; body: unknown }): string {
  if (c.body === null) return t('wizard.save.removed')
  return isNew(c) ? t('wizard.save.added') : t('wizard.save.changed')
}

async function save(): Promise<void> {
  busy.value = true
  error.value = null
  try {
    const ov = await putObjects(changes.value)
    w.reset(ov.objects)
    await site.load()
    emit('saved')
  } catch (err) {
    error.value = formError(err, t)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <p class="text-sm text-ink-2">{{ t('wizard.save.intro') }}</p>
    <p v-if="changes.length === 0" class="text-sm">
      {{ t('wizard.save.nothing') }}
    </p>
    <ul v-else class="flex flex-col text-sm">
      <li
        v-for="c in changes"
        :key="`${c.kind}/${c.id}`"
        class="flex gap-3 border-t border-line py-1.5 first:border-t-0"
      >
        <span class="w-24 text-ink-2">{{ kinds[c.kind]?.() }}</span>
        <span class="flex-1 font-medium">{{ c.id }}</span>
        <span class="text-ink-2">{{ what(c) }}</span>
      </li>
    </ul>
    <ProblemList v-if="error" :error="error" />
    <button
      type="button"
      class="min-h-11 self-start rounded-control bg-accent px-4 text-sm font-medium text-on-accent disabled:opacity-60"
      :disabled="busy || changes.length === 0"
      @click="save"
    >
      {{ t('wizard.save.submit') }}
    </button>
  </div>
</template>
