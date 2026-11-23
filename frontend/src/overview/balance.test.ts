import { describe, expect, it } from 'vitest'
import { balance, capacity, type Row } from './balance'
import { r4 } from './fixtures'
import { powerState, type Lookup } from './power'

const lookup = (m: Record<string, number>): Lookup => (k) => m[k]
const state = (m: Record<string, number>) => powerState(r4, lookup(m))
const kinds = (r: Row) => r.segments.map((s) => s.kind)
const sum = (r: Row) => r.segments.reduce((s, x) => s + x.share, 0)

const day = {
  'site/pv.power': 3000,
  'site/grid_in.power': 0,
  'site/grid_out.power': 500,
  'site/battery_in.power': 600,
  'site/battery_out.power': 0,
  'site/house_rest.power': 700,
  'site/loadpoints/wallbox.power': 1200,
  'backup/power': 0,
}

describe('balance', () => {
  it('charging and export are sinks, in order loadpoints, house, battery, grid', () => {
    const b = balance(state(day))
    expect(kinds(b.from)).toEqual(['pv'])
    expect(b.to.items.map((i) => i.kind)).toEqual(['wallbox', 'house', 'battery', 'grid'])
    expect(b.to.items.find((i) => i.kind === 'battery')?.power).toBe(600)
    expect(b.to.items.find((i) => i.kind === 'grid')?.power).toBe(500)
  })
  it('discharging and import are sources', () => {
    const b = balance(
      state({
        ...day,
        'site/grid_in.power': 400,
        'site/grid_out.power': 0,
        'site/battery_in.power': 0,
        'site/battery_out.power': 300,
      }),
    )
    expect(kinds(b.from)).toEqual(['pv', 'battery', 'grid'])
    expect(b.to.items.map((i) => i.kind)).toEqual(['wallbox', 'house'])
  })
  it('0 W items stay listed but have no segment', () => {
    const b = balance(state(day))
    expect(b.to.items.map((i) => i.kind)).toContain('wallbox')
    const z = balance(state({ ...day, 'site/loadpoints/wallbox.power': 0 }))
    expect(z.to.items.map((i) => i.kind)).toContain('wallbox')
    expect(kinds(z.to)).not.toContain('wallbox')
    expect(z.from.items.map((i) => i.kind)).toContain('pv')
  })
  it('missing power gives no segment', () => {
    const b = balance(state({ 'site/pv.power': 1000, 'site/house_rest.power': 1000 }))
    expect(kinds(b.from)).toEqual(['pv'])
    expect(kinds(b.to)).toEqual(['house'])
    expect(b.from.items.map((i) => i.kind)).toEqual(['pv', 'battery', 'grid'])
  })
  it('all missing: no segments, total 0', () => {
    const b = balance(state({}))
    expect(b.from.segments).toEqual([])
    expect(b.to.total).toBe(0)
  })
  it.each([
    ['many', day],
    ['single', { 'site/pv.power': 800 }],
    ['below 1 kW', { 'site/pv.power': 0.3, 'site/house_rest.power': 0.2, 'site/grid_in.power': 0.1, 'site/grid_out.power': 0 }],
    ['tiny', { 'site/pv.power': 1e-6, 'site/house_rest.power': 3e-7 }],
  ])('shares sum to 100 (%s)', (_n, m) => {
    const b = balance(state(m))
    for (const r of [b.from, b.to]) {
      if (r.total > 0) expect(Math.abs(sum(r) - 100)).toBeLessThan(1e-9)
      else expect(r.segments).toEqual([])
    }
  })
})

describe('capacity', () => {
  it('reserves battery and grid in both rows', () => {
    const c = capacity(state(day))
    expect(c.from.map((i) => i.kind).sort()).toEqual(['battery', 'grid', 'pv'])
    expect(c.to.map((i) => i.kind)).toEqual(expect.arrayContaining(['wallbox', 'house', 'battery', 'grid']))
    expect(c.to.map((i) => i.kind)).not.toContain('pv')
  })
})
