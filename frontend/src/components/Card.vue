<script setup lang="ts">
defineProps<{ title?: string }>()
</script>

<template>
  <section class="glass rounded-card p-4 md:p-5">
    <h2 v-if="title" class="mb-3 text-lg font-semibold">{{ title }}</h2>
    <slot />
  </section>
</template>
