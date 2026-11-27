import { describe, expect, it } from 'vitest'
import type { Tokens } from '@/charts/tokens'
import { historyOption, tooltipHTML, type HistoryText } from './historyOption'
import type { PeriodPoint } from './points'

const tokens = { 'ink-2': 'ink2', well: 'well', pv: 'pv', home: 'home', grid: 'grid', ev: 'ev' } as Tokens
const text: HistoryText = {
  lang: 'de',
  names: { pv: 'PV', consumption: 'Verbrauch <x>', gridIn: 'Bezug', gridOut: 'Einspeisung' } as HistoryText['names'],
  noData: 'keine Daten',
  incomplete: (days) => `fehlt: ${days.join(', ')}`,
  estimated: 'geschätzt',
  axis: (p) => p.start.slice(8),
  title: (p) => p.start,
}
const point = (start: string, pv: number | null): PeriodPoint => ({
  start,
  values: { pv, consumption: 2000, house: null, gridIn: null, gridOut: null } as PeriodPoint['values'],
  selfSufficiency: null,
  missing: [],
  estimated: false,
})

interface Bar {
  data: ({ value: number; itemStyle: { opacity: number; borderRadius: number[] | 0 } } | null)[]
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
    expect(pv?.data[0]).toEqual({ value: 4, itemStyle: { opacity: 1, borderRadius: [4, 4, 0, 0] } })
    expect(pv?.data[1]).toBeNull()
    expect(cons?.data[1]?.itemStyle.opacity).toBeLessThan(1)
  })

  it('stacks house and loadpoints in place of the consumption', () => {
    const split = point('2026-11-18', null)
    split.values = { ...split.values, house: -500, wallbox: 2500 }
    const old = point('2026-11-19', null)
    old.values = { ...old.values, wallbox: 100 }
    const data = (keys: Parameters<typeof historyOption>[1]) =>
      (historyOption([split, old], keys, null, tokens, text, false) as { series: Bar[] }).series.map(
        (s) => s.data.map((d) => d?.value ?? null),
      )
    expect(data(['consumption', 'house', 'wallbox'])).toEqual([
      [null, 2],
      [-0.5, null],
      [2.5, null],
    ])
    expect(data(['consumption'])).toEqual([[2, 2]])
    // Only the segment on top is round; the negative house value below it is not.
    const o = historyOption([split, old], ['consumption', 'house', 'wallbox'], null, tokens, text, false)
    const radius = (o as { series: Bar[] }).series.map((s) => s.data.map((d) => d?.itemStyle.borderRadius))
    expect(radius).toEqual([
      [undefined, [4, 4, 0, 0]],
      [0, undefined],
      [[4, 4, 0, 0], undefined],
    ])
  })

  it('escapes the tooltip and shows missing values', () => {
    const html = tooltipHTML(pts[1]!, ['pv', 'consumption'], tokens, text)
    expect(html).toContain('background:pv')
    expect(html).toContain('keine Daten')
    expect(html).toContain('Verbrauch &#60;x&#62;')
  })

  it('marks incomplete periods on the axis and names the days in the tooltip', () => {
    const p = { ...point('2026-11-18', 4000), missing: ['2026-11-18'], estimated: true }
    const o = historyOption([p], ['pv'], null, tokens, text, false) as { xAxis: { data: string[] } }
    expect(o.xAxis.data).toEqual(['18*'])
    const html = tooltipHTML(p, ['pv'], tokens, text)
    expect(html).toContain('* fehlt: 2026-11-18')
    expect(html).toContain('≈ geschätzt')
  })
})
