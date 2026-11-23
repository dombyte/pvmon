<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Lang } from '@/format'
import { languages } from '@/i18n'
import { usePreferencesStore } from '@/stores/preferences'
import SelectField from './select/SelectField.vue'

const { t } = useI18n()
const prefs = usePreferencesStore()
const options = computed(() => languages.map((l) => ({ value: l, label: t(`language.${l}`) })))
const language = computed<Lang>({
  get: () => prefs.language,
  set: (l) => (prefs.language = l),
})
</script>

<template>
  <SelectField v-model="language" :options="options" :label="t('language.label')" />
</template>
