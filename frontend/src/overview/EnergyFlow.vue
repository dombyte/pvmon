<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useResizeObserver } from '@/composables/useResizeObserver'
import Tick from '@/components/Tick.vue'
import { formatPower, type Lang } from '@/format'
import { layout, type Edge } from './flow'
import type { PowerState, RolePower } from './power'
import { roleColor, roleIcon } from './roles'
import { useNodeLabel } from './useNodeLabel'

const props = defineProps<{ state: PowerState }>()
const emit = defineEmits<{ select: [node: RolePower] }>()
const { locale } = useI18n()
const label = useNodeLabel()

const box = ref<HTMLElement | null>(null)
const width = ref(800)
useResizeObserver(box, (e) => (width.value = Math.round(e.contentRect.width)))

const geo = computed(() => layout(props.state.nodes.map((n) => n.kind), width.value))
const byKind = computed(() => new Map(props.state.nodes.map((n) => [n.kind, n])))

/** Flow along the polyline (node → hub) is "in"; from the hub to the node is "out". */
function direction(e: Edge): 'in' | 'out' | 'idle' | 'missing' {
  const p = byKind.value.get(e.kind)?.power
  if (p === undefined) return 'missing'
  if (p === 0) return 'idle'
  const source = e.kind === 'pv' || e.kind === 'grid' || e.kind === 'battery'
  return (p > 0) === source ? 'in' : 'out'
}
/** Dots run faster the more power flows (log scale: 100 W slow, 10 kW quick). */
function speed(e: Edge): string {
  const p = Math.abs(byKind.value.get(e.kind)?.power ?? 0)
  const slowest = 1.6
  const fastest = 0.45
  return `${Math.max(fastest, slowest - Math.log10(1 + p / 100) * 0.5)}s`
}
const path = (e: Edge) => e.points.map(([x, y], i) => `${i ? 'L' : 'M'}${x} ${y}`).join(' ')
const fmt = (w: number | undefined) => formatPower(w, locale.value as Lang)
</script>

<template>
  <div ref="box" class="w-full">
    <svg
      :viewBox="`-2 -2 ${geo.width + 4} ${geo.height + 4}`"
      class="block h-auto w-full overflow-visible"
      role="group"
    >
      <path
        v-for="e in geo.edges"
        :key="`e-${e.kind}`"
        :d="path(e)"
        fill="none"
        stroke-width="3"
        stroke-linecap="round"
        stroke-linejoin="round"
        :class="['edge', direction(e)]"
        :style="{
          stroke: direction(e) === 'missing' ? 'var(--line)' : roleColor[e.kind],
          animationDuration: speed(e),
        }"
      />
      <circle
        :cx="geo.hub.x"
        :cy="geo.hub.y"
        :r="geo.hub.r"
        fill="var(--tile)"
        stroke="var(--ink-2)"
        stroke-width="2"
      />
      <foreignObject
        v-for="b in geo.nodes"
        :key="`n-${b.kind}`"
        :x="b.x"
        :y="b.y"
        :width="b.w"
        :height="b.h"
      >
        <button
          v-if="byKind.get(b.kind)"
          type="button"
          class="node flex size-full items-center gap-3 rounded-[16px] border border-line bg-tile px-3 text-left"
          :class="{ 'opacity-50': byKind.get(b.kind)?.power === undefined }"
          :title="label(byKind.get(b.kind)!, state.soc, state.socApprox)"
          @click="emit('select', byKind.get(b.kind)!)"
        >
          <span
            class="tint grid size-11 shrink-0 place-items-center rounded-full"
            :style="{ color: roleColor[b.kind] }"
          >
            <component :is="roleIcon[b.kind]" :size="22" aria-hidden="true" />
          </span>
          <span class="flex min-w-0 flex-col">
            <span class="truncate text-[13px] text-ink-2">
              {{ label(byKind.get(b.kind)!, state.soc, state.socApprox) }}
            </span>
            <span class="text-[22px] font-semibold tabular-nums leading-tight">
              <Tick :text="fmt(Math.abs(byKind.get(b.kind)?.power ?? NaN))" />
            </span>
          </span>
        </button>
      </foreignObject>
    </svg>
  </div>
</template>

<style scoped>
.edge {
  stroke-dasharray: 2 9;
  transition: stroke 0.4s ease;
}
.edge.in {
  animation: flow 0.9s linear infinite;
}
.edge.out {
  animation: flow 0.9s linear infinite reverse;
}
.tint {
  background: color-mix(in srgb, currentcolor 14%, transparent);
}
.node {
  transition:
    background-color 0.15s ease,
    opacity 0.4s ease,
    transform 0.15s ease;
}
.node:hover {
  background: var(--well);
}
.node:active {
  transform: scale(0.98);
}
.node:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
@keyframes flow {
  to {
    stroke-dashoffset: -11;
  }
}
@media (prefers-reduced-motion: reduce) {
  .edge.in,
  .edge.out {
    animation: none;
  }
}
</style>
