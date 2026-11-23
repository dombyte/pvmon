import { describe, expect, it } from 'vitest'
import { formatChannel } from './channel'

describe('formatChannel', () => {
  it('formats by unit', () => {
    const f = (v: number | string | undefined, u?: string) => formatChannel(v, u, 'de').replace(/\s/g, ' ')
    expect(f(undefined, 'W')).toBe('–')
    expect(f('charging')).toBe('charging')
    expect(f(1500, 'W')).toBe('1,5 kW')
    expect(f(1.5, 'kW')).toBe('1,5 kW')
    expect(f(12345, 'Wh')).toBe('12,3 kWh')
    expect(f(2.5, 'kWh')).toBe('2,5 kWh')
    expect(f(64, '%')).toBe('64 %')
    expect(f(230.45, 'V')).toBe('230,5 V')
    expect(f(1.234)).toBe('1,23')
    expect(f(49.987, 'Hz')).toBe('49,99 Hz')
    expect(f(3.14159, 'x')).toBe('3,14 x')
  })
})
