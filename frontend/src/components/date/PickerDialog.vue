<script setup lang="ts">
// The modal dialog of the date pickers (spec 11: one look for every picker): centred on
// desktop, a bottom sheet in the compact layout. The content exists only while it is open.
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from '@lucide/vue'
import { useLayout } from '@/layout/mode'

defineProps<{
  /** Accessible name and dialog heading. */
  label: string
}>()
const { t } = useI18n()
const { mode } = useLayout()
const dialog = ref<HTMLDialogElement | null>(null)
const open = ref(false)

function show(): void {
  open.value = true
  dialog.value?.showModal()
}
function close(): void {
  dialog.value?.close()
}
defineExpose({ show, close })
</script>

<template>
  <dialog
    ref="dialog"
    class="picker glass rounded-card p-0 text-ink"
    :class="mode === 'compact' ? 'sheet mx-0 mt-auto mb-0 w-full max-w-none rounded-b-none' : 'm-auto w-80'"
    :aria-label="label"
    @click.self="close"
    @close="open = false"
  >
    <div class="flex flex-col gap-2 p-4">
      <div class="flex items-center justify-between">
        <h2 class="text-base font-semibold">{{ label }}</h2>
        <button
          type="button"
          class="grid size-10 place-items-center rounded-control hover:bg-well"
          :aria-label="t('common.close')"
          @click="close"
        >
          <X :size="18" aria-hidden="true" />
        </button>
      </div>
      <slot v-if="open" />
    </div>
  </dialog>
</template>

<style scoped>
.picker::backdrop {
  background: rgb(0 0 0 / 0.35);
}
</style>
