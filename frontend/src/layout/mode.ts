import { computed } from 'vue'
import { useMediaQuery } from '@/composables/useMediaQuery'

const compactMaxWidth = 768

export type LayoutMode = 'compact' | 'desktop'

export interface LayoutInput {
  coarse: boolean
  width: number
}

/** Compact layout when the pointer is coarse or the width is below 768 px (spec 11.2). */
export function layoutMode({ coarse, width }: LayoutInput): LayoutMode {
  return coarse || width < compactMaxWidth ? 'compact' : 'desktop'
}

/** The phone variant of the flow only when coarse and narrow (tablets keep desktop widgets). */
export function isPhone({ coarse, width }: LayoutInput): boolean {
  return coarse && width < compactMaxWidth
}

export function useLayout() {
  const coarse = useMediaQuery('(pointer: coarse)')
  const narrow = useMediaQuery(`(max-width: ${compactMaxWidth - 0.02}px)`)
  // The media query stands in for the width: narrow means width < 768.
  const input = computed<LayoutInput>(() => ({
    coarse: coarse.value,
    width: narrow.value ? compactMaxWidth - 1 : compactMaxWidth,
  }))
  return {
    mode: computed(() => layoutMode(input.value)),
    phone: computed(() => isPhone(input.value)),
  }
}
