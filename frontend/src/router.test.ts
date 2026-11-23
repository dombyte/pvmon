import { describe, expect, it } from 'vitest'
import { redirectFor } from './router'

const page = (name: string, admin = false) => ({ name, fullPath: `/${name}?x=1`, meta: { admin } })
const open = { setupRequired: false, loggedIn: false, canRead: true }
const closed = { setupRequired: false, loggedIn: false, canRead: false }
const admin = { setupRequired: false, loggedIn: true, canRead: true }

describe('redirectFor', () => {
  it('sends everything to setup while no user exists', () => {
    const s = { ...closed, setupRequired: true }
    expect(redirectFor(page('overview'), s)).toEqual({ name: 'setup' })
    expect(redirectFor(page('login'), s)).toEqual({ name: 'setup' })
    expect(redirectFor(page('setup'), s)).toBe(true)
  })

  it('leaves setup once it is done', () => {
    expect(redirectFor(page('setup'), open)).toEqual({ name: 'overview' })
  })

  it('opens data pages with public read and asks for a login otherwise', () => {
    expect(redirectFor(page('day'), open)).toBe(true)
    expect(redirectFor(page('day'), closed)).toEqual({
      name: 'login',
      query: { next: '/day?x=1' },
    })
  })

  it('needs a login for admin pages', () => {
    expect(redirectFor(page('settings', true), open)).toEqual({
      name: 'login',
      query: { next: '/settings?x=1' },
    })
    expect(redirectFor(page('settings', true), admin)).toBe(true)
  })

  it('skips the login page when logged in', () => {
    expect(redirectFor(page('login'), admin)).toEqual({ name: 'overview' })
    expect(redirectFor(page('login'), closed)).toBe(true)
  })
})
