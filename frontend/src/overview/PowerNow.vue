<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDownToLine, ArrowUpFromLine, BatteryMedium, ChevronDown } from '@lucide/vue'
import Card from '@/components/Card.vue'
import Tick from '@/components/Tick.vue'
import { readStored, writeStored } from '@/composables/storage'
import { formatPercent, formatPower, type Lang } from '@/format'
import { balance, capacity, type Item, type Row } from './balance'
import type { PowerState, RolePower } from './power'
import { roleColor, roleIcon } from './roles'
import { useNodeLabel } from './useNodeLabel'

const props = defineProps<{ state: PowerState }>()
const emit = defineEmits<{ select: [node: RolePower] }>()
const { t, locale } = useI18n()
const label = useNodeLabel()
const rows = computed(() => balance(props.state))
const room = computed(() => capacity(props.state))
const battery = computed(() => props.state.nodes.find((n) => n.kind === 'battery'))
const socText = computed(
  () => `${props.state.socApprox ? '≈' : ''}${formatPercent(props.state.soc, locale.value as Lang)}`,
)
const fmt = (w: number | undefined) => formatPower(w, locale.value as Lang)
// The widest value a legend entry shows: the reserved room never runs short.
const widest = computed(() => fmt(99_900))
type Section = { key: 'from' | 'to'; title: string; row: Row; icon: typeof ArrowDownToLine }
const sections = computed<Section[]>(() => [
  { key: 'from', title: t('power.from'), row: rows.value.from, icon: ArrowDownToLine },
  { key: 'to', title: t('power.to'), row: rows.value.to, icon: ArrowUpFromLine },
])
/** Grid and battery show their bare name here: the row already says the direction. */
const name = (i: Item) =>
  i.kind === 'battery' || i.kind === 'grid' ? t(`roles.${i.kind}`) : label(i)
const dark = (kind: Item['kind']) => `color-mix(in srgb, ${roleColor[kind]} 40%, black)`

const item = (row: Row, kind: Item['kind']) => row.items.find((i) => i.kind === kind)
const segmentLabel = (row: Row, kind: Item['kind']) => {
  const i = item(row, kind)
  return i ? `${name(i)} ${fmt(i.power)}` : ''
}
function open(row: Row, kind: Item['kind']): void {
  const i = item(row, kind)
  if (i) emit('select', i as RolePower)
}

// Bars only (values folded away), remembered per browser. Only the title button folds.
const storeKey = 'pvmon.power.bars'
const barsOnly = ref(readStored(storeKey) === '1')
function toggle(): void {
  barsOnly.value = !barsOnly.value
  writeStored(storeKey, barsOnly.value ? '1' : '0')
}
</script>

<template>
  <Card>
    <div class="flex items-center justify-between gap-3" :class="barsOnly ? 'mb-2' : 'mb-3'">
      <h2 class="text-base font-semibold">
        <button
          type="button"
          class="flex min-h-9 items-center gap-1.5"
          :aria-expanded="!barsOnly"
          :aria-label="barsOnly ? t('power.expand') : t('power.collapse')"
          @click="toggle"
        >
          {{ t('power.title') }}
          <ChevronDown
            :size="16"
            class="chevron text-ink-2"
            :class="{ folded: barsOnly }"
            aria-hidden="true"
          />
        </button>
      </h2>
      <button
        v-if="battery"
        type="button"
        class="flex min-h-9 items-center gap-1.5 rounded-full border border-line bg-well px-3 text-[13px] font-semibold tabular-nums"
        :aria-label="t('power.socChip', { value: socText })"
        @click="emit('select', battery)"
      >
        <BatteryMedium :size="16" :style="{ color: roleColor.battery }" aria-hidden="true" />
        {{ socText }}
      </button>
    </div>
    <div
      v-for="s in sections"
      :key="s.key"
      class="section"
      :class="barsOnly ? 'mb-3 last:mb-0' : 'mb-4 last:mb-0'"
    >
      <!-- Folded, the row keeps its title; only the total fades. -->
      <div class="mb-1.5 flex items-center justify-between text-sm font-semibold">
        <span class="flex items-center gap-1.5">
          <component :is="s.icon" :size="16" aria-hidden="true" />
          {{ s.title }}
        </span>
        <span class="fade tabular-nums" :class="{ folded: barsOnly }">
          <Tick :text="fmt(s.row.total)" />
        </span>
      </div>
      <div class="flex h-7 gap-0.5 overflow-hidden rounded-chip bg-well">
        <!-- Each segment opens its breakdown, like its entry in the list. -->
        <button
          v-for="seg in s.row.segments"
          :key="seg.kind"
          type="button"
          class="segment grid min-w-0 place-items-center"
          :style="{ flexGrow: seg.share, flexBasis: 0, background: roleColor[seg.kind] }"
          :aria-label="segmentLabel(s.row, seg.kind)"
          @click="open(s.row, seg.kind)"
        >
          <component
            :is="roleIcon[seg.kind]"
            :size="16"
            class="seg-icon"
            :style="{ color: dark(seg.kind) }"
            aria-hidden="true"
          />
        </button>
      </div>
      <div class="fold" :class="{ folded: barsOnly }">
        <div class="min-h-0 overflow-hidden">
          <!-- The visible legend over an invisible one holding every entry this row can
               ever show: the card keeps its height when the battery or grid turns around. -->
          <div class="legend mt-1.5 text-[13px]">
            <ul class="flex flex-wrap gap-x-3">
              <li v-for="i in s.row.items" :key="i.kind">
                <button
                  type="button"
                  class="flex min-h-6 items-center gap-1.5"
                  @click="emit('select', i as RolePower)"
                >
                  <span class="size-2 rounded-full" :style="{ background: roleColor[i.kind] }" />
                  <span class="text-ink-2">{{ name(i) }}</span>
                  <span class="font-semibold tabular-nums"><Tick :text="fmt(i.power)" /></span>
                </button>
              </li>
            </ul>
            <ul class="invisible flex flex-wrap gap-x-3" aria-hidden="true">
              <li v-for="i in room[s.key]" :key="i.kind" class="flex min-h-6 items-center gap-1.5">
                <span class="size-2" />
                <span>{{ name(i) }}</span>
                <span class="font-semibold">{{ widest }}</span>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  </Card>
</template>

<style scoped>
/* Icons only where the segment is at least 26 px wide (spec 11.6). */
.segment {
  container-type: inline-size;
  transition: flex-grow 0.6s ease;
}
@container (max-width: 25.9px) {
  .seg-icon {
    display: none;
  }
}

/* Both legends in one grid cell: the cell is as tall as the taller one. */
.legend {
  display: grid;
}
.legend > * {
  grid-area: 1 / 1;
}

/* Folding values away: rows collapse smoothly to zero height. */
.fold {
  display: grid;
  grid-template-rows: 1fr;
  transition:
    grid-template-rows 0.25s ease,
    opacity 0.2s ease,
    visibility 0.25s;
}
/* Hidden once folded, so the values leave the accessibility tree too. */
.fold.folded {
  grid-template-rows: 0fr;
  opacity: 0;
  visibility: hidden;
}
.fade {
  transition:
    opacity 0.2s ease,
    visibility 0.2s;
}
.fade.folded {
  opacity: 0;
  visibility: hidden;
}
.chevron {
  transition: transform 0.25s ease;
}
.chevron.folded {
  transform: rotate(-90deg);
}
</style>
