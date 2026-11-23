<script setup lang="ts" generic="T extends string">
import SelectField from '@/components/select/SelectField.vue'

// A select with its label shown above it, like TextField, for the wizard's forms.
defineProps<{ options: { value: T; label: string }[]; label: string }>()
const model = defineModel<T>({ required: true })
</script>

<template>
  <div class="flex flex-col gap-1 text-sm">
    <span class="text-ink-2" aria-hidden="true">{{ label }}</span>
    <SelectField v-model="model" :options="options" :label="label" class="w-full" />
  </div>
</template>
