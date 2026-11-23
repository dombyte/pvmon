// ECharts options of the day view (spec 11.3, 11.7): generation and consumption (PV with
// an optional min/max band, consumption, loadpoints per category or per meter), grid
// (import up, export down) and battery (charge up, discharge down, SoC line). Pure:
// everything comes in as arguments, so it is tested without a DOM.
import type { EChartsCoreOption } from 'echarts/core'
import type { Tokens } from '@/charts/tokens'
import { tooltipBody, tooltipRow } from '@/charts/tooltip'
import { formatNumber, formatPercent, formatPower, type Lang } from '@/format'
import type { DaySlot } from './model'

/** One loadpoint line: a whole category or one meter of a split category. */
export interface LoadLine {
  /** Key in DaySlot.loadpoints. */
  key: string
  name: string
  token: keyof Tokens
  /** Meters of one category share its colour and differ by dash. */
  dash: 'solid' | 'dashed' | 'dotted'
}

export interface DayText {
  lang: Lang
  pv: string
  pvBand: string
  consumption: string
  /** Visible loadpoint lines. */
  loadpoints: LoadLine[]
  gridIn: string
  gridOut: string
  batteryOut: string
  batteryIn: string
  soc: string
  noData: string
}

/** Series the chips can hide; the PV band is off unless asked for. */
export interface DayVisible {
  pv: boolean
  band: boolean
  consumption: boolean
  grid: boolean
  battery: boolean
}

const wPerKw = 1000
const gridPad = 8
// Fixed side margins keep the time axes of the stacked charts aligned for the crosshair
// (the battery chart has a second axis on the right).
const sideLeft = 44
const sideRight = 40
const labelEvery = 12 // one axis label per 3 hours
const bandOpacity = 0.3
const areaOpacity = 0.18
const fullPercent = 100

/** "13:00–13:15" of a slot. */
export function slotRange(slots: DaySlot[], i: number): string {
  const end = slots[i + 1]?.time ?? '24:00'
  return `${slots[i]?.time ?? ''}–${end}`
}

const kw = (w: number | null) => (w === null ? null : w / wPerKw)
const power = (w: number | null, x: DayText) => (w === null ? x.noData : formatPower(w, x.lang))

type Formatter = (s: DaySlot) => string[]

function base(slots: DaySlot[], t: Tokens, x: DayText, animate: boolean, rows: Formatter) {
  return {
    animation: animate,
    grid: { left: sideLeft, right: sideRight, top: gridPad, bottom: 24, outerBoundsMode: 'none' },
    xAxis: {
      type: 'category',
      // Values are slot averages: points sit in the middle of their slot, like the bars.
      boundaryGap: true,
      data: slots.map((s) => s.time),
      axisLabel: { interval: labelEvery - 1, hideOverlap: true },
    },
    tooltip: {
      trigger: 'axis',
      triggerOn: 'mousemove|click',
      confine: true,
      axisPointer: { type: 'line', lineStyle: { color: t['ink-2'], type: 'dashed' } },
      formatter: (p: { dataIndex: number }[]) => {
        const i = p[0]?.dataIndex ?? -1
        const s = slots[i]
        return s ? tooltipBody(slotRange(slots, i), rows(s)) : ''
      },
    },
    yAxis: {
      type: 'value',
      axisLabel: { formatter: (v: number) => formatNumber(v, x.lang, 1, 0) },
    },
  }
}

function line(name: string, color: string, data: (number | null)[], area = false) {
  return {
    type: 'line',
    name,
    data,
    showSymbol: false,
    connectNulls: false,
    lineStyle: { width: 2, color },
    itemStyle: { color },
    ...(area ? { areaStyle: { color, opacity: areaOpacity } } : {}),
  }
}

/** The PV band: an invisible min line with the min–max span stacked on top. */
function band(slots: DaySlot[], t: Tokens): object[] {
  const b = { type: 'line', stack: 'band', showSymbol: false, lineStyle: { opacity: 0 }, silent: true }
  return [
    { ...b, name: 'min', data: slots.map((s) => kw(s.pvMin)) },
    {
      ...b,
      name: 'band',
      data: slots.map((s) => (s.pvMax === null || s.pvMin === null ? null : kw(s.pvMax - s.pvMin))),
      areaStyle: { color: t.pv, opacity: bandOpacity },
    },
  ]
}

function powerRows(vis: DayVisible, t: Tokens, x: DayText): Formatter {
  return (s) => {
    const rows: string[] = []
    if (vis.pv) rows.push(tooltipRow(x.pv, power(s.pv, x), t.pv))
    if (vis.pv && vis.band && s.pvMin !== null && s.pvMax !== null) {
      rows.push(tooltipRow(x.pvBand, `${power(s.pvMin, x)} – ${power(s.pvMax, x)}`))
    }
    if (vis.consumption) rows.push(tooltipRow(x.consumption, power(s.consumption, x), t.home))
    for (const l of x.loadpoints) {
      rows.push(tooltipRow(l.name, power(s.loadpoints[l.key] ?? null, x), t[l.token]))
    }
    return rows
  }
}

/** Generation and consumption: PV (area, or line plus band), consumption, loadpoints. */
export function powerOption(
  slots: DaySlot[],
  vis: DayVisible,
  t: Tokens,
  x: DayText,
  animate: boolean,
): EChartsCoreOption {
  const series: object[] = []
  if (vis.pv && vis.band) series.push(...band(slots, t))
  if (vis.pv) series.push(line(x.pv, t.pv, slots.map((s) => kw(s.pv)), !vis.band))
  if (vis.consumption) series.push(line(x.consumption, t.home, slots.map((s) => kw(s.consumption))))
  for (const l of x.loadpoints) {
    const s = line(l.name, t[l.token], slots.map((v) => kw(v.loadpoints[l.key] ?? null)))
    series.push({ ...s, lineStyle: { ...s.lineStyle, type: l.dash } })
  }
  const b = base(slots, t, x, animate, powerRows(vis, t, x))
  return { ...b, yAxis: { ...b.yAxis, min: 0 }, series }
}

/** Grid as bars: import above zero, export below. */
export function gridOption(slots: DaySlot[], t: Tokens, x: DayText, animate: boolean): EChartsCoreOption {
  const b = base(slots, t, x, animate, (s) => {
    const g = s.grid
    const [name, v] = g !== null && g < 0 ? [x.gridOut, -g] : [x.gridIn, g]
    return [tooltipRow(name, power(v, x), t.grid)]
  })
  return {
    ...b,
    series: [{ type: 'bar', name: x.gridIn, data: slots.map((s) => kw(s.grid)), itemStyle: { color: t.grid } }],
  }
}

/** Battery as bars (charge above zero, discharge below) and, when known, the SoC line. */
export function batteryOption(
  slots: DaySlot[],
  withSoc: boolean,
  t: Tokens,
  x: DayText,
  animate: boolean,
): EChartsCoreOption {
  const b = base(slots, t, x, animate, (s) => {
    const v = s.battery
    const [name, w] = v !== null && v > 0 ? [x.batteryOut, v] : [x.batteryIn, v === null ? null : -v]
    const rows = [tooltipRow(name, power(w, x), t.battery)]
    if (withSoc) {
      rows.push(tooltipRow(x.soc, s.soc === null ? x.noData : formatPercent(s.soc, x.lang), t.ink))
    }
    return rows
  })
  const series: object[] = [
    {
      type: 'bar',
      name: x.batteryIn,
      // The slot keeps + discharge; the chart shows charging upwards.
      data: slots.map((s) => (s.battery === null ? null : -s.battery / wPerKw)),
      itemStyle: { color: t.battery },
    },
  ]
  const percent = { formatter: (v: number) => formatNumber(v, x.lang, 0, 0) }
  const yAxis: object[] = [b.yAxis]
  if (withSoc) {
    series.push({ ...line(x.soc, t.ink, slots.map((s) => s.soc)), yAxisIndex: 1 })
    yAxis.push({ type: 'value', min: 0, max: fullPercent, splitLine: { show: false }, axisLabel: percent })
  }
  return { ...b, yAxis, series }
}
