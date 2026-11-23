import { describe, expect, it } from 'vitest'
import type { EnergySeries, GaugeSeries, SiteModel } from '@/api/types'
import { dayQuality, dayRequest, daySlots } from './model'

const site = (series: string[], meters: object[] = []) =>
  ({ site_series: series, meters }) as unknown as SiteModel
const at = (h: string) => `2026-11-19T${h}:00+01:00`
const energy = (name: string, wh: (number | null)[]): EnergySeries => ({
  name,
  unit: 'Wh',
  values: wh.map((v, i) => ({
    period: at(['12:00', '12:15'][i] ?? ''),
    wh: v,
    complete: v !== null,
    estimated: false,
    missing_days: 0,
    missing: [],
  })),
})

describe('day model', () => {
  it('requests what the site has', () => {
    const full = dayRequest(
      site(['site/pv', 'site/consumption', 'site/grid_in', 'site/grid_out', 'site/soc',
        'site/loadpoints/wallbox']),
    )
    expect(full.energy).toEqual(['site/pv', 'site/consumption', 'site/grid_in', 'site/grid_out',
      'site/loadpoints/wallbox'])
    expect(full.gauge).toEqual(['site/pv', 'site/soc'])
    expect(full.categories).toEqual(['wallbox'])
    expect(dayRequest(site(['site/consumption'])).gauge).toEqual([])
  })

  it('turns Wh per slot into W, nets grid and battery, takes PV from the gauge', () => {
    const req = dayRequest(
      site(['site/pv', 'site/consumption', 'site/grid_in', 'site/grid_out', 'site/battery_in',
        'site/battery_out', 'site/soc', 'site/loadpoints/wallbox']),
    )
    const e = Object.fromEntries(
      [
        energy('site/pv', [500, null]),
        energy('site/consumption', [300, null]),
        energy('site/grid_in', [0, null]),
        energy('site/grid_out', [100, null]),
        energy('site/battery_in', [150, null]),
        energy('site/battery_out', [0, null]),
        energy('site/loadpoints/wallbox', [50, null]),
      ].map((s) => [s.name, s]),
    )
    const g: Record<string, GaugeSeries> = {
      'site/pv': { name: 'site/pv', unit: 'W', points: [{ at: at('12:00'), avg: 1900, min: 1500, max: 2600, cover: 900 }] },
      'site/soc': { name: 'site/soc', unit: '%', points: [{ at: at('12:15'), avg: 64, min: 60, max: 66, cover: 900 }] },
    }
    const [a, b] = daySlots(e, g, req)
    expect(a).toEqual({
      at: at('12:00'), time: '12:00', pv: 1900, pvMin: 1500, pvMax: 2600, consumption: 1200,
      loadpoints: { wallbox: 200 }, grid: -400, battery: -600, soc: null,
    })
    expect(b).toMatchObject({ pv: null, grid: null, battery: null, soc: 64 })
    // Without the gauge the PV line falls back to the counters.
    expect(daySlots(e, {}, req)[0]?.pv).toBe(2000)
    expect(dayQuality([a!, b!])).toEqual({ complete: 1, total: 2 })
    expect(daySlots({}, {}, { energy: [], gauge: [], categories: [], meters: [] })).toEqual([])
  })

  it('offers the meters of categories with several loadpoints one by one', () => {
    const wb = (id: string, extra: object = {}) => ({
      id, role: 'consumer', category: 'wallbox', label: id.toUpperCase(), device: 'd',
      series: [`${id}/power`, `${id}/energy_in`], ...extra,
    })
    const req = dayRequest(site(['site/consumption', 'site/loadpoints/wallbox', 'site/loadpoints/heatpump'], [
      wb('wb1', { parent: 'site' }), wb('wb2'), wb('sub', { parent: 'wb1' }),
      { id: 'hp', role: 'consumer', category: 'heatpump', label: 'HP', device: 'd', series: ['hp/power'] },
    ]))
    expect(req.meters).toEqual([
      { id: 'wb1', category: 'wallbox', label: 'WB1' },
      { id: 'wb2', category: 'wallbox', label: 'WB2' },
    ])
    expect(req.gauge).toEqual(['wb1/power', 'wb2/power'])
    const g: Record<string, GaugeSeries> = {
      'wb1/power': { name: 'wb1/power', unit: 'W', points: [{ at: at('12:00'), avg: 11000, min: 0, max: 11000, cover: 900 }] },
    }
    const [a] = daySlots({ c: energy('site/consumption', [300]) }, g, { ...req, energy: ['c'] })
    expect(a?.loadpoints).toMatchObject({ 'm:wb1': 11000, 'm:wb2': null })
  })
})
