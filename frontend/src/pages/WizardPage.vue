<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { errorKey, fetchConfig, fetchProfiles } from '@/api/client'
import Card from '@/components/Card.vue'
import Page from '@/components/Page.vue'
import CheckStep from '@/wizard/CheckStep.vue'
import DevicesStep from '@/wizard/DevicesStep.vue'
import { changesOf } from '@/wizard/draft'
import HierarchyStep from '@/wizard/HierarchyStep.vue'
import MetersStep from '@/wizard/MetersStep.vue'
import SaveStep from '@/wizard/SaveStep.vue'
import { createWizard } from '@/wizard/state'

// The setup wizard (spec 11.14 steps 2–6, admin only): it edits a draft of the UI
// configuration and saves it at the end as one revision.
const { t } = useI18n()
const w = createWizard()
const loaded = ref(false)
const error = ref<string | null>(null)
const step = ref(0)
const saved = ref(false)

// Keys spelled out for the i18n key check.
const steps = [
  { title: () => t('wizard.steps.devices'), component: DevicesStep },
  { title: () => t('wizard.steps.meters'), component: MetersStep },
  { title: () => t('wizard.steps.hierarchy'), component: HierarchyStep },
  { title: () => t('wizard.steps.check'), component: CheckStep },
  { title: () => t('wizard.steps.save'), component: SaveStep },
]
const current = computed(() => steps[step.value]!)
const dirty = computed(() => changesOf(w.orig.value, w.draft.value).length > 0)

onMounted(async () => {
  try {
    const [cfg, prof] = await Promise.all([fetchConfig(true), fetchProfiles(true)])
    w.reset(cfg.objects)
    w.profiles.value = prof.profiles
    loaded.value = true
  } catch (err) {
    error.value = errorKey(err)
  }
})

function go(i: number): void {
  saved.value = false
  step.value = i
}
</script>

<template>
  <Page :title="t('wizard.title')">
    <p v-if="error" class="text-sm text-fault" role="alert">{{ t(error) }}</p>
    <template v-if="loaded">
      <nav :aria-label="t('wizard.steps.label')">
        <ol class="flex flex-wrap gap-2">
          <li v-for="(s, i) in steps" :key="i">
            <button
              type="button"
              class="min-h-11 rounded-control px-3 text-sm"
              :class="i === step ? 'bg-accent text-on-accent' : 'border border-line'"
              :aria-current="i === step ? 'step' : undefined"
              @click="go(i)"
            >
              {{ i + 1 }}. {{ s.title() }}
            </button>
          </li>
        </ol>
      </nav>
      <Card :title="current.title()">
        <p v-if="saved" class="mb-3 text-sm" role="status">
          {{ t('wizard.save.done') }}
          <RouterLink :to="{ name: 'overview' }" class="text-accent underline">{{
            t('nav.overview')
          }}</RouterLink>
        </p>
        <component :is="current.component" v-else @saved="saved = true" />
      </Card>
      <div class="flex flex-wrap items-center gap-3">
        <button
          v-if="step > 0"
          type="button"
          class="min-h-11 rounded-control border border-line px-4 text-sm"
          @click="go(step - 1)"
        >
          {{ t('wizard.back') }}
        </button>
        <button
          v-if="step < steps.length - 1"
          type="button"
          class="min-h-11 rounded-control bg-accent px-4 text-sm font-medium text-on-accent"
          @click="go(step + 1)"
        >
          {{ t('wizard.next') }}
        </button>
        <p v-if="dirty" class="text-sm text-ink-2">{{ t('wizard.unsaved') }}</p>
      </div>
    </template>
  </Page>
</template>
