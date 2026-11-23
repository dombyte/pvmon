// ECharts option of the 14-day trend (spec 11.4, 11.7). Pure: everything the chart shows
// comes in as arguments, so it is tested without a DOM or ECharts.
import type { EChartsCoreOption } from 'echarts/core'
import type { Tokens } from '@/charts/tokens'
import { escapeHTML, tooltipBody, tooltipRow } from '@/charts/tooltip'
import { formatEnergy, formatLocalDate, formatNumber, formatPercent, type Lang } from '@/format'
import type { DayPoint, TrendKey } from './trend'

export interface TrendText {
  lang: Lang
  /** Series labels by key. */
  names: Record<TrendKey, string>
  selfSufficiency: string
  noData: string
  running: string
  /** Footer hint: a click pins the day below the chart. */
  pickHint: string
}

export const tokenOf: Record<TrendKey, 'pv' | 'home'> = { pv: 'pv', consumption: 'home' }

const whPerKwh = 1000
const barMaxWidth = 14
const barRadius = 4
const gridPad = 8
const dimmed = 0.35


/** Tooltip body of one day: every visible series, self-sufficiency and the click hint. */
export function tooltipHTML(p: DayPoint, keys: TrendKey[], t: Tokens, x: TrendText): string {
  const date = formatLocalDate(p.date, x.lang, { weekday: 'short', day: 'numeric', month: 'short' })
  const head = p.running ? `${date} · ${x.running}` : date
  const value = (v: number | null, fmt: (n: number) => string) => (v === null ? x.noData : fmt(v))
  const rows = keys.map((k) =>
    tooltipRow(x.names[k], value(p[k], (n) => formatEnergy(n, x.lang)), t[tokenOf[k]]),
  )
  rows.push(tooltipRow(x.selfSufficiency, value(p.selfSufficiency, (n) => formatPercent(n, x.lang))))
  // The tooltip follows the pointer, so it holds no link; a click pins the day instead.
  return tooltipBody(head, rows, `<span style="opacity:.7">${escapeHTML(x.pickHint)}</span>`)
}

/**
 * Bars per visible key in kWh. A day without data has no bar (a gap, never a 0 bar,
 * spec 11.7); its tooltip says so.
 */
export function trendOption(
  points: DayPoint[],
  keys: TrendKey[],
  t: Tokens,
  x: TrendText,
  view: { animate: boolean; selected: number | null },
): EChartsCoreOption {
  const { animate, selected } = view
  return {
    animation: animate,
    grid: { left: gridPad, right: gridPad, top: gridPad, bottom: gridPad, outerBoundsMode: 'same' },
    xAxis: {
      type: 'category',
      data: points.map((p) => formatLocalDate(p.date, x.lang, { day: 'numeric' })),
    },
    yAxis: { type: 'value', min: 0, axisLabel: { formatter: (v: number) => formatNumber(v, x.lang, 1, 0) } },
    tooltip: {
      trigger: 'axis',
      triggerOn: 'mousemove|click',
      confine: true,
      axisPointer: { type: 'shadow', shadowStyle: { color: t.well } },
      formatter: (params: { dataIndex: number }[]) => {
        const p = points[params[0]?.dataIndex ?? -1]
        return p ? tooltipHTML(p, keys, t, x) : ''
      },
    },
    series: keys.map((k) => ({
      type: 'bar',
      name: x.names[k],
      barMaxWidth,
      itemStyle: { color: t[tokenOf[k]], borderRadius: [barRadius, barRadius, 0, 0] },
      // While a day is pinned the others step back.
      data: points.map((p, i) => ({
        value: p[k] === null ? null : p[k] / whPerKwh,
        itemStyle: { opacity: selected === null || selected === i ? 1 : dimmed },
      })),
    })),
  }
}
