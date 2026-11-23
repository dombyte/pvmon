// Graphite tokens for charts (spec 11.7, 11.9): ECharts needs real colours, so they are
// read from the CSS custom properties and re-read whenever the theme changes.
import { nextTick, onScopeDispose, shallowRef, watch, type Ref } from 'vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { usePreferencesStore } from '@/stores/preferences'

const names = ['ink', 'ink-2', 'tile', 'line', 'well', 'pv', 'home', 'grid', 'ev', 'heatpump', 'heater', 'other', 'battery', 'accent'] as const
type Token = (typeof names)[number]
export type Tokens = Record<Token, string>

function readTokens(): Tokens {
  const style = getComputedStyle(document.documentElement)
  return Object.fromEntries(
    names.map((n) => [n, style.getPropertyValue(`--${n}`).trim()]),
  ) as Tokens
}

/** Current token values; updates after a theme choice or an OS scheme change. */
export function useTokens(): Ref<Tokens> {
  const prefs = usePreferencesStore()
  const dark = useMediaQuery('(prefers-color-scheme: dark)')
  const tokens = shallowRef(readTokens())
  // The store sets data-theme in its own watcher; read after the DOM has it.
  const stop = watch([() => prefs.theme, dark], () => {
    void nextTick(() => (tokens.value = readTokens()))
  })
  onScopeDispose(stop)
  return tokens
}

/** The ECharts theme built from the tokens (axes, labels, tooltip). */
export function chartTheme(t: Tokens): object {
  const label = { color: t['ink-2'], fontSize: 12 }
  return {
    backgroundColor: 'transparent',
    textStyle: { color: t['ink-2'], fontFamily: "'Inter Variable', system-ui, sans-serif" },
    categoryAxis: {
      axisLine: { lineStyle: { color: t.line } },
      axisTick: { show: false },
      axisLabel: label,
      splitLine: { show: false },
    },
    valueAxis: {
      axisLine: { show: false },
      axisLabel: label,
      splitLine: { lineStyle: { color: t.line } },
    },
    tooltip: {
      backgroundColor: t.tile,
      borderColor: t.line,
      textStyle: { color: t.ink, fontSize: 13 },
    },
  }
}

/** Token of a loadpoint category; wallboxes use `ev`, unknown categories `other`. */
export function categoryToken(cat: string): 'ev' | 'heatpump' | 'heater' | 'other' {
  switch (cat) {
    case 'wallbox':
      return 'ev'
    case 'heatpump':
    case 'heater':
      return cat
    default:
      return 'other'
  }
}
