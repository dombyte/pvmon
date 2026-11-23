<script setup lang="ts">
import { useId } from 'vue'

// A labelled input; `error` is announced and tied to the input (spec 11.13).
defineProps<{
  label: string
  type?: 'text' | 'password' | 'number'
  autocomplete?: string
  required?: boolean
  disabled?: boolean
  hint?: string
  error?: string
}>()
const model = defineModel<string>({ required: true })
const id = useId()
</script>

<template>
  <div class="flex flex-col gap-1 text-sm">
    <label :for="id" class="text-ink-2">{{ label }}</label>
    <input
      :id="id"
      v-model="model"
      :type="type ?? 'text'"
      :autocomplete="autocomplete"
      :required="required"
      :disabled="disabled"
      :aria-invalid="error ? 'true' : undefined"
      :aria-describedby="error || hint ? `${id}-msg` : undefined"
      class="min-h-11 rounded-control border border-line bg-tile px-3 text-base text-ink disabled:opacity-60"
    />
    <p v-if="error" :id="`${id}-msg`" class="text-xs text-fault">{{ error }}</p>
    <p v-else-if="hint" :id="`${id}-msg`" class="text-xs text-ink-2">{{ hint }}</p>
  </div>
</template>
