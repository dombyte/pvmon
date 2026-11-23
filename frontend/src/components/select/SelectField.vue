<script setup lang="ts" generic="T extends string">
// The one select of the app (spec 11.7): a button with a themed listbox popup (WAI-ARIA
// select-only combobox), so the open list matches the theme instead of the platform's.
import { Check, ChevronDown } from '@lucide/vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { moveIndex, opensUp, typeAhead } from './listbox'

const props = defineProps<{ options: { value: T; label: string }[]; label: string }>()
const model = defineModel<T>({ required: true })

const id = useId()
const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)
const list = ref<HTMLUListElement | null>(null)
const open = ref(false)
const up = ref(false)
// The list is fixed-positioned in the nearest landmark, outside the glass cards: they are
// stacking contexts and would cover it.
const host = ref<HTMLElement | null>(null)
onMounted(() => (host.value = root.value?.closest<HTMLElement>('main, aside, nav') ?? document.body))
const place = ref<Record<string, string>>({})
const active = ref(0)
const selected = computed(() => Math.max(0, props.options.findIndex((o) => o.value === model.value)))
const current = computed(() => props.options[selected.value]?.label ?? '')
const listHeight = 240
let typed = ''
let typedAt = 0
const typePause = 700

function outside(e: PointerEvent): void {
  const t = e.target as Node
  if (!root.value?.contains(t) && !list.value?.contains(t)) close(false)
}
const gap = 4
function measure(): void {
  const r = button.value?.getBoundingClientRect()
  if (!r) return
  up.value = opensUp(r, listHeight, window.innerHeight)
  place.value = {
    left: `${r.left}px`,
    minWidth: `${r.width}px`,
    ...(up.value
      ? { bottom: `${window.innerHeight - r.top + gap}px` }
      : { top: `${r.bottom + gap}px` }),
  }
}
// Scrolling or resizing would leave the list behind its button.
const dismiss = (e: Event) => {
  if (!list.value?.contains(e.target as Node)) close(false)
}
async function show(): Promise<void> {
  if (open.value) return
  measure()
  active.value = selected.value
  open.value = true
  document.addEventListener('pointerdown', outside)
  window.addEventListener('resize', dismiss)
  document.addEventListener('scroll', dismiss, true)
  await nextTick()
  // No scroll on focus: a scroll closes the list (it would leave its button behind).
  list.value?.focus({ preventScroll: true })
}
function close(focus = true): void {
  open.value = false
  unlisten()
  if (focus) button.value?.focus()
}
function choose(i: number): void {
  const o = props.options[i]
  if (o) model.value = o.value
  close()
}
function unlisten(): void {
  document.removeEventListener('pointerdown', outside)
  window.removeEventListener('resize', dismiss)
  document.removeEventListener('scroll', dismiss, true)
}
onBeforeUnmount(unlisten)

function onButtonKey(e: KeyboardEvent): void {
  if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(e.key)) {
    e.preventDefault()
    void show()
  }
}
function onType(key: string): void {
  const now = performance.now()
  typed = now - typedAt > typePause ? key : typed + key
  typedAt = now
  // A longer prefix may still match the active option, a fresh letter moves on.
  const from = typed.length > 1 ? active.value - 1 : active.value
  const hit = typeAhead(
    props.options.map((o) => o.label),
    typed,
    from,
  )
  if (hit !== null) active.value = hit
}
function onListKey(e: KeyboardEvent): void {
  const to = moveIndex(active.value, e.key, props.options.length)
  if (to !== null) {
    e.preventDefault()
    active.value = to
  } else if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    choose(active.value)
  } else if (e.key === 'Escape') {
    e.preventDefault()
    close()
  } else if (e.key === 'Tab') {
    close(false)
  } else if (e.key.length === 1) {
    onType(e.key)
  }
}
</script>

<template>
  <div ref="root" class="relative inline-flex">
    <button
      ref="button"
      type="button"
      class="flex min-h-11 w-full items-center justify-between gap-2 rounded-control border border-line bg-tile pr-3 pl-3 text-sm hover:bg-well"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="`${id}-list`"
      :aria-label="`${label}: ${current}`"
      @click="open ? close() : show()"
      @keydown="onButtonKey"
    >
      <span class="truncate">{{ current }}</span>
      <ChevronDown
        :size="16"
        class="shrink-0 text-ink-2 transition-transform duration-200"
        :class="open ? 'rotate-180' : ''"
        aria-hidden="true"
      />
    </button>
    <Teleport v-if="host" :to="host">
      <Transition name="pop">
        <ul
          v-if="open"
          :id="`${id}-list`"
          ref="list"
          role="listbox"
          tabindex="-1"
          :aria-label="label"
          :aria-activedescendant="`${id}-${active}`"
          class="glass-solid fixed z-50 max-h-60 overflow-y-auto rounded-control p-1 text-ink"
          :class="up ? 'origin-bottom' : 'origin-top'"
          :style="place"
          @keydown="onListKey"
        >
          <li
            v-for="(o, i) in options"
            :id="`${id}-${i}`"
            :key="o.value"
            role="option"
            :aria-selected="i === selected"
            class="flex min-h-10 cursor-pointer items-center justify-between gap-3 rounded-chip px-3 text-sm whitespace-nowrap"
            :class="[i === active ? 'bg-well' : '', i === selected ? 'font-semibold' : '']"
            @pointerenter="active = i"
            @click="choose(i)"
          >
            {{ o.label }}
            <Check v-if="i === selected" :size="16" class="text-accent" aria-hidden="true" />
          </li>
        </ul>
      </Transition>
    </Teleport>
  </div>
</template>
