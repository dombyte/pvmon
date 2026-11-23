import { computed, ref, watch, type Ref } from 'vue'
import { readStored, writeStored } from '@/composables/storage'
import type { LocalDate } from '@/format/localdate'
import {
  defaultLength,
  granularities,
  latest,
  presets,
  shift,
  type Granularity,
  type Range,
} from './range'

const storageKey = 'pvmon.history'

interface Stored {
  gran: Granularity
  n: Record<Granularity, number>
}

/** Reads the last granularity and preset per granularity; anything invalid is dropped. */
export function readPrefs(raw: string | null): Stored {
  const out: Stored = { gran: 'day', n: { ...defaultLength } }
  try {
    const v = JSON.parse(raw ?? '{}') as Partial<Stored>
    if (granularities.includes(v.gran as Granularity)) out.gran = v.gran as Granularity
    for (const g of granularities) {
      const n = v.n?.[g]
      if (typeof n === 'number' && presets[g].includes(n)) out.n[g] = n
    }
  } catch {
    // A broken entry falls back to the defaults.
  }
  return out
}

/** The history range; granularity and preset are stored per browser (spec 11.8). */
export function useHistoryRange(today: Ref<LocalDate>) {
  const prefs = ref(readPrefs(readStored(storageKey)))
  const end = ref<LocalDate | null>(null)
  /** Length of a custom range; not stored, presets are. */
  const customN = ref<number | null>(null)
  const range = computed<Range | null>(() => {
    if (!today.value) return null
    const n = customN.value ?? prefs.value.n[prefs.value.gran]
    const r = latest(prefs.value.gran, n, today.value)
    return end.value ? { ...r, end: end.value } : r
  })
  watch(prefs, (p) => writeStored(storageKey, JSON.stringify(p)), { deep: true })

  function setGranularity(g: Granularity): void {
    prefs.value.gran = g
    end.value = null
    customN.value = null
  }
  function setLength(n: number): void {
    prefs.value.n[prefs.value.gran] = n
    end.value = null
    customN.value = null
  }
  /** A custom range of the current granularity. */
  function setCustom(r: Range): void {
    customN.value = r.n
    end.value = r.end >= today.value ? null : r.end
  }
  function move(dir: -1 | 1): void {
    if (!range.value) return
    const next = shift(range.value, dir, today.value)
    end.value = next.end >= today.value ? null : next.end
  }
  return { range, custom: computed(() => customN.value !== null), setGranularity, setLength, setCustom, move }
}
