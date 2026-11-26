<script setup lang="ts">
// A date button that opens the themed calendar to pick one day.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatLocalDate, type Lang } from '@/format'
import type { LocalDate } from '@/format/localdate'
import CalendarGrid from './CalendarGrid.vue'
import PickerButton from './PickerButton.vue'
import PickerDialog from './PickerDialog.vue'
import { selectable } from './calendar'

const props = defineProps<{
  modelValue: LocalDate
  /** Accessible name and dialog heading. */
  label: string
  min?: LocalDate
  max?: LocalDate
  today?: LocalDate
  /** Button text; the medium date by default. */
  display?: string
}>()
const emit = defineEmits<{ 'update:modelValue': [LocalDate] }>()
// Two roots (button and dialog): classes go to the button.
defineOptions({ inheritAttrs: false })
const { t, locale } = useI18n()
const dialog = ref<InstanceType<typeof PickerDialog> | null>(null)
const text = computed(
  () => props.display ?? formatLocalDate(props.modelValue, locale.value as Lang),
)

function pick(d: LocalDate): void {
  emit('update:modelValue', d)
  dialog.value?.close()
}
</script>

<template>
  <PickerButton v-bind="$attrs" :label="label" :text="text" @click="dialog?.show()" />
  <PickerDialog ref="dialog" :label="label">
    <CalendarGrid :value="modelValue" :min="min" :max="max" :today="today" @pick="pick" />
    <button
      v-if="today && selectable(today, min, max)"
      type="button"
      class="min-h-10 rounded-control border border-line text-sm hover:bg-well"
      @click="pick(today)"
    >
      {{ t('day.today') }}
    </button>
  </PickerDialog>
</template>
