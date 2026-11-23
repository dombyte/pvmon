import { describe, expect, it } from 'vitest'
import { isOutdated } from './version'

describe('isOutdated', () => {
  it('flags a different released version', () => {
    expect(isOutdated('1.0.0-abc', '1.1.0-def')).toBe(true)
    expect(isOutdated('1.0.0-abc', '1.0.0-abc')).toBe(false)
  })
  it('never reports for dev or before the first answer', () => {
    expect(isOutdated('dev', '1.0.0-abc')).toBe(false)
    expect(isOutdated('1.0.0-abc', 'dev')).toBe(false)
    expect(isOutdated('1.0.0-abc', null)).toBe(false)
  })
})
