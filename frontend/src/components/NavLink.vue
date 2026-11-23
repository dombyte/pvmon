<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { isActive, type NavItem } from '@/layout/nav'

const props = defineProps<{ item: NavItem; stacked?: boolean }>()
const { t } = useI18n()
const route = useRoute()
const active = computed(() => isActive(props.item, route.path))
</script>

<template>
  <RouterLink
    :to="item.to"
    :aria-current="active ? 'page' : undefined"
    class="flex min-h-11 items-center rounded-control text-base font-medium"
    :class="[
      stacked ? 'flex-1 flex-col justify-center gap-0.5 px-2 py-1 text-xs' : 'gap-3 px-3',
      active ? 'toggle-on' : 'text-ink-2 hover:bg-well',
    ]"
  >
    <component :is="item.icon" :size="stacked ? 22 : 20" aria-hidden="true" />
    <span>{{ t(item.labelKey) }}</span>
  </RouterLink>
</template>
