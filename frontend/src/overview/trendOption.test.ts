import { describe, expect, it } from 'vitest'
import type { Tokens } from '@/charts/tokens'
import type { DayPoint } from './trend'
import { tooltipHTML, trendOption, type TrendText } from './trendOption'

const tokens = {
  ink: 'ink',
  'ink-2': 'ink2',
  tile: 'tile',
  line: 'line',
  well: 'well',
  pv: 'pv',
  home: 'home',
  grid: 'grid',
  accent: 'accent',
} as Tokens

const text: TrendText = {
  lang: 'de',
  names: { pv: 'PV', consumption: 'Verbrauch <x>' },
  selfSufficiency: 'Autarkie',
  noData: 'keine Daten',
  running: 'läuft',
  pickHint: 'Klicken für Details',
  incomplete: 'unvollständig',
  estimated: 'geschätzt',
}

const point = (date: string, pv: number | null, running = false): DayPoint => ({
  date,
  running,
  pv,
  consumption: 12600,
  gridIn: 6100,
  gridOut: 300,
  selfSufficiency: pv === null ? null : 52,
  incomplete: false,
  estimated: false,
})

interface Bar {
  data: { value: number | null; itemStyle: { opacity: number } }[]
  itemStyle: { color: string }
}

describe('trendOption', () => {
  const points = [point('2026-11-17', 8600), point('2026-11-18', null), point('2026-11-19', 500, true)]

  it('leaves a gap for missing days', () => {
    const opt = trendOption(points, ['pv', 'consumption'], tokens, text, { animate: true, selected: null })
    const [pv, cons] = opt.series as Bar[]
    expect(pv?.itemStyle.color).toBe('pv')
    expect(cons?.itemStyle.color).toBe('home')
    expect(pv?.data[0]?.value).toBe(8.6)
    expect(pv?.data[1]?.value).toBeNull()
    expect((opt.xAxis as { data: string[] }).data).toEqual(['17', '18', '19'])
  })

  it('scales only over visible series', () => {
    const opt = trendOption(points, ['pv'], tokens, text, { animate: false, selected: 0 })
    expect(opt.series).toHaveLength(1)
    const bars = (opt.series as Bar[])[0]?.data
    expect(bars?.[1]?.value).toBeNull()
    expect(bars?.map((b) => b.itemStyle.opacity)).toEqual([1, 0.35, 0.35])
    expect(opt.animation).toBe(false)
  })

  it('shows the values, the click hint and escapes labels', () => {
    const done = tooltipHTML(point('2026-11-18', 6200), ['pv', 'consumption'], tokens, text)
    expect(done).not.toContain('<a')
    expect(done).toContain('Klicken für Details')
    expect(done).toContain('6,2\u00a0kWh')
    expect(done).toContain('52\u00a0%')
    expect(done).toContain('Verbrauch &#60;x&#62;')
    const running = tooltipHTML(point('2026-11-19', null, true), ['pv'], tokens, text)
    expect(running).not.toContain('<a')
    expect(running).toContain('läuft')
    expect(running).toContain('keine Daten')
  })

  it('marks incomplete and estimated days on the axis and in the tooltip', () => {
    const p = { ...point('2026-11-18', 6200), incomplete: true, estimated: true }
    const opt = trendOption([p], ['pv'], tokens, text, { animate: false, selected: null })
    expect((opt.xAxis as { data: string[] }).data).toEqual(['18*'])
    const html = tooltipHTML(p, ['pv'], tokens, text)
    expect(html).toContain('* unvollständig')
    expect(html).toContain('≈ geschätzt')
    expect(tooltipHTML(point('2026-11-18', 6200), ['pv'], tokens, text)).not.toContain('unvollständig')
  })
})
