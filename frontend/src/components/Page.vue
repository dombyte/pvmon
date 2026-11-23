<script setup lang="ts">
defineProps<{ title: string }>()
</script>

<template>
  <div class="flex flex-col gap-4 p-4 md:p-6">
    <h1 class="text-xl font-semibold">{{ title }}</h1>
    <slot />
  </div>
</template>
