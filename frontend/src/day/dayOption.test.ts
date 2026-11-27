import { describe, expect, it } from 'vitest'
import type { Tokens } from '@/charts/tokens'
import { batteryOption, gridOption, powerOption, slotRange, type DayText } from './dayOption'
import type { DaySlot } from './model'

const tokens = {
  'ink-2': 'ink2', ink: 'ink', pv: 'pv', home: 'home', grid: 'grid', ev: 'ev', battery: 'bat',
} as Tokens
const text: DayText = {
  lang: 'de', pv: 'PV Ø', pvBand: 'PV min–max', consumption: 'Verbrauch <x>', house: 'Haus',
  loadpoints: [
    { key: 'm:wb1', name: 'Garage', token: 'ev', opacity: 0.75 },
    { key: 'm:wb2', name: 'Einfahrt', token: 'ev', opacity: 0.3 },
  ],
  gridIn: 'Netzbezug', gridOut: 'Einspeisung',
  batteryOut: 'Entladen', batteryIn: 'Laden', soc: 'Ladestand', noData: 'keine Daten',
}
const slot = (time: string, v: Partial<DaySlot> = {}): DaySlot => ({
  at: `2026-11-19T${time}:00+01:00`, time, pv: 2000, pvMin: 1500, pvMax: 2600,
  consumption: 1200, house: 1000, loadpoints: { 'm:wb1': 200, 'm:wb2': 0 }, grid: -400, battery: 600, soc: 64, ...v,
})
const slots = [slot('12:00'), slot('12:15', { pv: null, pvMin: null, grid: 300, battery: -100, soc: null })]
const all = { pv: true, band: true, house: true, grid: true, battery: true }

type Opt = {
  series: { name: string; data: (number | null)[]; areaStyle?: object; stack?: string; lineStyle?: { width?: number } }[]
  yAxis: object | object[]
  tooltip: { formatter: (p: { dataIndex: number }[]) => string }
}

describe('day options', () => {
  it('draws the PV band only on request, with the PV line on top', () => {
    const o = powerOption(slots, all, tokens, text, false) as unknown as Opt
    expect(o.series.map((s) => s.name)).toEqual(['min', 'band', 'PV Ø', 'Haus', 'Garage', 'Einfahrt'])
    expect(o.series[1]?.data).toEqual([1.1, null])
    expect(o.series[2]?.areaStyle).toBeUndefined()
    expect(o.series[5]?.areaStyle).toMatchObject({ opacity: 0.3 })
    expect(o.series[5]?.lineStyle?.width).toBe(0)
    const tip = o.tooltip.formatter([{ dataIndex: 0 }])
    expect(tip).toContain('12:00–12:15')
    expect(tip).toMatch(/1,5.kW – 2,6.kW/)
    expect(tip).toContain('Verbrauch &#60;x&#62;')
    expect(tip).toContain('Garage')
    expect(o.tooltip.formatter([{ dataIndex: 1 }])).toContain('12:15–24:00')
    expect(o.tooltip.formatter([])).toBe('')

    const plain = powerOption(slots, { ...all, band: false }, tokens, text, false) as unknown as Opt
    expect(plain.series[0]?.name).toBe('PV Ø')
    expect(plain.series[0]?.areaStyle).toBeUndefined()
    expect(plain.series.slice(1).map((s) => s.stack)).toEqual(['use', 'use', 'use'])
    expect(plain.tooltip.formatter([{ dataIndex: 0 }])).not.toContain('min–max')
    const none = powerOption(slots, { ...all, pv: false, house: false }, tokens,
      { ...text, loadpoints: [] }, false) as unknown as Opt
    expect(none.series).toEqual([])
  })

  it('draws no part of a stack without a balance, and keeps a negative house', () => {
    const odd = [slot('12:00', { house: null }), slot('12:15', { house: -300 })]
    const o = powerOption(odd, all, tokens, text, false) as unknown as Opt
    expect(o.series.at(-3)?.data).toEqual([null, -0.3])
    expect(o.series.at(-2)?.data).toEqual([null, 0.2])
    expect((o.yAxis as { min: (v: { min: number }) => number }).min({ min: -0.3 })).toBe(-0.3)
  })

  it('shows grid import up and export down', () => {
    const o = gridOption(slots, tokens, text, false) as unknown as Opt
    expect(o.series[0]?.data).toEqual([-0.4, 0.3])
    expect(o.tooltip.formatter([{ dataIndex: 0 }])).toContain('Einspeisung')
    expect(o.tooltip.formatter([{ dataIndex: 1 }])).toContain('Netzbezug')
  })

  it('shows battery charge up, discharge down and the SoC on its own axis', () => {
    const o = batteryOption(slots, true, tokens, text, false) as unknown as Opt
    expect(o.series[0]?.data).toEqual([-0.6, 0.1])
    expect(o.series[1]?.data).toEqual([64, null])
    expect(o.yAxis).toHaveLength(2)
    expect(o.tooltip.formatter([{ dataIndex: 0 }])).toMatch(/Entladen.*Ladestand/)
    expect(o.tooltip.formatter([{ dataIndex: 1 }])).toMatch(/Laden.*keine Daten/)
    const noSoc = batteryOption(slots, false, tokens, text, false) as unknown as Opt
    expect(noSoc.series).toHaveLength(1)
    expect(noSoc.tooltip.formatter([{ dataIndex: 0 }])).not.toContain('Ladestand')
    expect(slotRange([], 0)).toBe('–24:00')
  })
})
