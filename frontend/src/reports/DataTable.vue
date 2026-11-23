<script setup lang="ts">
defineProps<{ header: string[]; rows: string[][]; label: string }>()
</script>

<template>
  <div class="overflow-x-auto" tabindex="0" role="region" :aria-label="label">
    <table class="w-full min-w-max text-sm tabular-nums">
      <thead>
        <tr class="border-b border-line">
          <th
            v-for="(h, i) in header"
            :key="i"
            scope="col"
            class="py-2 pr-4 font-semibold last:pr-0"
            :class="i === 0 ? 'text-left' : 'text-right'"
          >
            {{ h }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(r, i) in rows" :key="i" class="border-b border-line last:border-b-0">
          <td
            v-for="(c, j) in r"
            :key="j"
            class="py-2 pr-4 last:pr-0"
            :class="j === 0 ? 'text-left' : 'text-right'"
          >
            {{ c }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
