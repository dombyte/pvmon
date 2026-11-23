<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

// A destructive action in two steps instead of a browser dialog: the first click asks,
// the second one acts; leaving the button cancels.
defineProps<{ label: string; disabled?: boolean }>()
const emit = defineEmits<{ confirm: [] }>()
const { t } = useI18n()
const asking = ref(false)

function click(): void {
  if (!asking.value) {
    asking.value = true
    return
  }
  asking.value = false
  emit('confirm')
}
</script>

<template>
  <button
    type="button"
    class="min-h-11 rounded-control px-3 text-sm font-medium disabled:opacity-60"
    :class="asking ? 'bg-fault font-semibold text-on-accent' : 'text-ink-2 hover:bg-well'"
    :disabled="disabled"
    @click="click"
    @blur="asking = false"
  >
    {{ asking ? t('common.confirm', { action: label }) : label }}
  </button>
</template>
