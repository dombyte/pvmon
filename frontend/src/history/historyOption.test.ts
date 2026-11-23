import { describe, expect, it } from 'vitest'
import type { Tokens } from '@/charts/tokens'
import { historyOption, tooltipHTML, type HistoryText } from './historyOption'
import type { PeriodPoint } from './points'

const tokens = { 'ink-2': 'ink2', well: 'well', pv: 'pv', home: 'home', grid: 'grid', ev: 'ev' } as Tokens
const text: HistoryText = {
  lang: 'de',
  names: { pv: 'PV', consumption: 'Verbrauch <x>', gridIn: 'Bezug', gridOut: 'Einspeisung' },
  noData: 'keine Daten',
  axis: (p) => p.start.slice(8),
  title: (p) => p.start,
}
const point = (start: string, pv: number | null): PeriodPoint => ({
  start,
  values: { pv, consumption: 2000, gridIn: null, gridOut: null },
  selfSufficiency: null,
  incomplete: false,
  estimated: false,
})

interface Bar {
  data: ({ value: number; itemStyle: { opacity: number } } | null)[]
  itemStyle: { color: string }
}

describe('history option', () => {
  const pts = [point('2026-11-18', 4000), point('2026-11-19', null)]

  it('leaves gaps for missing periods and dims unselected bars', () => {
    const o = historyOption(pts, ['pv', 'consumption'], 0, tokens, text, false) as {
      series: Bar[]
      xAxis: { data: string[] }
    }
    expect(o.xAxis.data).toEqual(['18', '19'])
    const [pv, cons] = o.series
    expect(pv?.itemStyle.color).toBe('pv')
    expect(pv?.data[0]).toEqual({ value: 4, itemStyle: { opacity: 1 } })
    expect(pv?.data[1]).toBeNull()
    expect(cons?.data[1]?.itemStyle.opacity).toBeLessThan(1)
  })

  it('escapes the tooltip and shows missing values', () => {
    const html = tooltipHTML(pts[1]!, ['pv', 'consumption'], tokens, text)
    expect(html).toContain('background:pv')
    expect(html).toContain('keine Daten')
    expect(html).toContain('Verbrauch &#60;x&#62;')
  })
})
