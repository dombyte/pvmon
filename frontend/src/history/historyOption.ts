// ECharts option of the history bars (spec 11.7, 11.8). Pure: everything the chart shows
// comes in as arguments, so it is tested without a DOM or ECharts.
import type { EChartsCoreOption } from 'echarts/core'
import type { Tokens } from '@/charts/tokens'
import { escapeHTML, tooltipBody, tooltipRow } from '@/charts/tooltip'
import { formatEnergy, formatNumber, type Lang } from '@/format'
import { tokenOf, type HistoryKey, type PeriodPoint } from './points'

export interface HistoryText {
  lang: Lang
  names: Record<HistoryKey, string>
  noData: string
  /** Note of a period with missing days; gets the formatted day list. */
  incomplete: (days: string[]) => string
  estimated: string
  /** Axis label of a period. */
  axis: (p: PeriodPoint) => string
  /** Tooltip heading of a period. */
  title: (p: PeriodPoint) => string
}

const whPerKwh = 1000
const barMaxWidth = 14
const barRadius = 4
const gridPad = 8
const dimmed = 0.45

/** Tooltip body of one period: every visible series. */
export function tooltipHTML(p: PeriodPoint, keys: HistoryKey[], t: Tokens, x: HistoryText): string {
  const rows = keys.map((k) => {
    const v = p.values[k]
    return tooltipRow(x.names[k], v === null ? x.noData : formatEnergy(v, x.lang), t[tokenOf[k]])
  })
  const notes = [p.missing.length && `* ${x.incomplete(p.missing)}`, p.estimated && `≈ ${x.estimated}`]
  const foot = notes.filter(Boolean).map((n) => `<div style="opacity:.7">${escapeHTML(String(n))}</div>`)
  return tooltipBody(x.title(p), rows, foot.join(''))
}

/**
 * Bars per visible key in kWh. A period without data has no bar (a gap, never a 0 bar);
 * bars outside the selected period are dimmed while one is selected.
 */
export function historyOption(
  points: PeriodPoint[],
  keys: HistoryKey[],
  selected: number | null,
  t: Tokens,
  x: HistoryText,
  animate: boolean,
): EChartsCoreOption {
  return {
    animation: animate,
    grid: { left: gridPad, right: gridPad, top: gridPad, bottom: gridPad, outerBoundsMode: 'same' },
    // An incomplete period carries the same "*" as the energy table.
    xAxis: { type: 'category', data: points.map((p) => x.axis(p) + (p.missing.length ? '*' : '')) },
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
      data: points.map((p, i) => {
        const v = p.values[k]
        if (v === null) return null
        const opacity = selected === null || selected === i ? 1 : dimmed
        return { value: v / whPerKwh, itemStyle: { opacity } }
      }),
    })),
  }
}
