import type { EChartsCoreOption, EChartsType } from 'echarts/core'
import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { useResizeObserver } from '@/composables/useResizeObserver'
import { chartTheme, useTokens, type Tokens } from './tokens'

type Lib = typeof import('./echarts')

/**
 * Mounts an ECharts instance on `el` (spec 11.7). ECharts is imported on first use, the
 * option is rebuilt from `build` whenever its inputs or the theme change, the theme is
 * swapped at runtime and a tap outside the chart closes its tooltip. `onSelect` gets the
 * category index of a click or tap anywhere in the plot area (not only on a bar); charts
 * with the same `group` share crosshair and tooltip position.
 */
export function useChart(
  el: Ref<HTMLElement | null>,
  build: (t: Tokens, animate: boolean) => EChartsCoreOption | null,
  opts: { onSelect?: (index: number) => void; group?: string } = {},
) {
  const tokens = useTokens()
  const reducedMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
  const lib = shallowRef<Lib | null>(null)
  const failed = ref(false)
  let chart: EChartsType | null = null

  import('./echarts').then(
    (m) => (lib.value = m),
    () => (failed.value = true),
  )

  const option = computed(() => build(tokens.value, !reducedMotion.value))
  function render(): void {
    if (chart && option.value) chart.setOption(option.value, { notMerge: true })
  }

  watch(
    [lib, el],
    ([l, node]) => {
      chart?.dispose()
      chart = l && node ? l.init(node, chartTheme(tokens.value), { renderer: 'svg' }) : null
      if (chart && opts.onSelect) listenSelect(chart, opts.onSelect)
      if (chart && l && opts.group) {
        chart.group = opts.group
        l.connect(opts.group)
      }
      render()
    },
    { flush: 'post' },
  )
  function listenSelect(c: EChartsType, f: (index: number) => void): void {
    c.getZr().on('click', (e: { offsetX: number; offsetY: number }) => {
      const pt = [e.offsetX, e.offsetY]
      if (!c.containPixel('grid', pt)) return
      const [index] = c.convertFromPixel({ seriesIndex: 0 }, pt) as number[]
      if (index !== undefined && index >= 0) f(Math.round(index))
    })
  }
  watch(tokens, (t) => chart?.setTheme(chartTheme(t)))
  watch(option, render)
  useResizeObserver(el, () => chart?.resize())

  function onPointerDown(ev: PointerEvent): void {
    if (chart && el.value && !el.value.contains(ev.target as Node)) {
      chart.dispatchAction({ type: 'hideTip' })
    }
  }
  document.addEventListener('pointerdown', onPointerDown)
  onScopeDispose(() => {
    document.removeEventListener('pointerdown', onPointerDown)
    chart?.dispose()
    chart = null
  })
  return { failed }
}
