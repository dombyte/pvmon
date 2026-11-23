import { describe, expect, it } from 'vitest'
import { isPhone, layoutMode } from './mode'

describe('layoutMode', () => {
  it.each([
    [{ coarse: false, width: 1280 }, 'desktop'],
    [{ coarse: false, width: 768 }, 'desktop'],
    [{ coarse: false, width: 767 }, 'compact'],
    [{ coarse: true, width: 1280 }, 'compact'],
    [{ coarse: true, width: 390 }, 'compact'],
  ] as const)('%j', (input, want) => {
    expect(layoutMode(input)).toBe(want)
  })
  it('uses the phone flow only when coarse and narrow', () => {
    expect(isPhone({ coarse: true, width: 390 })).toBe(true)
    expect(isPhone({ coarse: true, width: 1024 })).toBe(false)
    expect(isPhone({ coarse: false, width: 390 })).toBe(false)
  })
})
