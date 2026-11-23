import { describe, expect, it } from 'vitest'
import { moveIndex, opensUp, typeAhead } from './listbox'

describe('listbox keys', () => {
  it('moves within the bounds', () => {
    expect(moveIndex(0, 'ArrowDown', 3)).toBe(1)
    expect(moveIndex(2, 'ArrowDown', 3)).toBe(2)
    expect(moveIndex(0, 'ArrowUp', 3)).toBe(0)
    expect(moveIndex(1, 'Home', 3)).toBe(0)
    expect(moveIndex(1, 'End', 3)).toBe(2)
    expect(moveIndex(1, 'PageDown', 3)).toBe(2)
    expect(moveIndex(1, 'PageUp', 3)).toBe(0)
    expect(moveIndex(1, 'x', 3)).toBeNull()
    expect(moveIndex(0, 'ArrowDown', 0)).toBeNull()
  })

  it('finds the next option by its first letters', () => {
    const l = ['Deutsch', 'English', 'Dansk']
    expect(typeAhead(l, 'd', 0)).toBe(2)
    expect(typeAhead(l, 'd', 2)).toBe(0)
    expect(typeAhead(l, 'EN', 0)).toBe(1)
    expect(typeAhead(l, 'x', 0)).toBeNull()
    expect(typeAhead(l, '', 0)).toBeNull()
  })

  it('opens upwards only when there is more room above', () => {
    expect(opensUp({ top: 700, bottom: 744 }, 120, 800)).toBe(true)
    expect(opensUp({ top: 100, bottom: 144 }, 120, 800)).toBe(false)
    expect(opensUp({ top: 30, bottom: 74 }, 120, 120)).toBe(false)
  })
})
