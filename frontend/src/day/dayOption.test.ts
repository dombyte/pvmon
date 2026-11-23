import { describe, expect, it } from 'vitest'
import type { Tokens } from '@/charts/tokens'
import { batteryOption, gridOption, powerOption, slotRange, type DayText } from './dayOption'
import type { DaySlot } from './model'

const tokens = {
  'ink-2': 'ink2', ink: 'ink', pv: 'pv', home: 'home', grid: 'grid', ev: 'ev', battery: 'bat',
} as Tokens
const text: DayText = {
  lang: 'de', pv: 'PV Ø', pvBand: 'PV min–max', consumption: 'Verbrauch <x>',
  loadpoints: [
    { key: 'm:wb1', name: 'Garage', token: 'ev', dash: 'solid' },
    { key: 'm:wb2', name: 'Einfahrt', token: 'ev', dash: 'dashed' },
  ],
  gridIn: 'Netzbezug', gridOut: 'Einspeisung',
  batteryOut: 'Entladen', batteryIn: 'Laden', soc: 'Ladestand', noData: 'keine Daten',
}
const slot = (time: string, v: Partial<DaySlot> = {}): DaySlot => ({
  at: `2026-11-19T${time}:00+01:00`, time, pv: 2000, pvMin: 1500, pvMax: 2600,
  consumption: 1200, loadpoints: { 'm:wb1': 200, 'm:wb2': 0 }, grid: -400, battery: 600, soc: 64, ...v,
})
const slots = [slot('12:00'), slot('12:15', { pv: null, pvMin: null, grid: 300, battery: -100, soc: null })]
const all = { pv: true, band: true, consumption: true, grid: true, battery: true }

type Opt = {
  series: { name: string; data: (number | null)[]; areaStyle?: object; lineStyle?: { type?: string } }[]
  yAxis: object | object[]
  tooltip: { formatter: (p: { dataIndex: number }[]) => string }
}

describe('day options', () => {
  it('draws the PV band only on request, with the PV line on top', () => {
    const o = powerOption(slots, all, tokens, text, false) as unknown as Opt
    expect(o.series.map((s) => s.name)).toEqual(['min', 'band', 'PV Ø', 'Verbrauch <x>', 'Garage', 'Einfahrt'])
    expect(o.series[1]?.data).toEqual([1.1, null])
    expect(o.series[2]?.areaStyle).toBeUndefined()
    expect(o.series[5]?.lineStyle?.type).toBe('dashed')
    const tip = o.tooltip.formatter([{ dataIndex: 0 }])
    expect(tip).toContain('12:00–12:15')
    expect(tip).toMatch(/1,5.kW – 2,6.kW/)
    expect(tip).toContain('Verbrauch &#60;x&#62;')
    expect(tip).toContain('Garage')
    expect(o.tooltip.formatter([{ dataIndex: 1 }])).toContain('12:15–24:00')
    expect(o.tooltip.formatter([])).toBe('')

    const plain = powerOption(slots, { ...all, band: false }, tokens, text, false) as unknown as Opt
    expect(plain.series[0]?.name).toBe('PV Ø')
    expect(plain.series[0]?.areaStyle).toBeDefined()
    expect(plain.tooltip.formatter([{ dataIndex: 0 }])).not.toContain('min–max')
    const none = powerOption(slots, { ...all, pv: false, consumption: false }, tokens,
      { ...text, loadpoints: [] }, false) as unknown as Opt
    expect(none.series).toEqual([])
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
