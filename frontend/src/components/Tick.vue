<script setup lang="ts">
// A live value that slides in when its text changes (spec 11.7, motion), in the compact
// layout only: on the desktop the values change in place.
import { useLayout } from '@/layout/mode'

defineProps<{ text: string }>()
const { mode } = useLayout()
</script>

<template>
  <span v-if="mode === 'compact'" :key="text" class="tick">{{ text }}</span>
  <template v-else>{{ text }}</template>
</template>
