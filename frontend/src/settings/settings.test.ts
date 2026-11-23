import { describe, expect, it } from 'vitest'
import type { SettingView } from '@/api/types'
import { changesOf, formOf } from './settings'

const view = (value: unknown, locked = false): SettingView => ({ value, locked, stored: false })
const settings: Record<string, SettingView> = {
  'site.name': view('Home'),
  'site.interval': view('5s', true),
  'status.offline_after': view('15m'),
  'auth.public_read': view(true),
  'storage.retention': view('10y'),
  'storage.backup_interval': view('24h'),
  'storage.max_backups': view(7),
}

describe('settings form', () => {
  it('turns values into form fields', () => {
    const f = formOf(settings)
    expect(f['site.name']).toBe('Home')
    expect(f['auth.public_read']).toBe(true)
    expect(f['storage.max_backups']).toBe('7')
    expect(formOf({})['site.name']).toBe('')
    expect(formOf({})['auth.public_read']).toBe(false)
  })

  it('sends only changed, unlocked keys with their types', () => {
    const before = formOf(settings)
    const after = {
      ...before,
      'site.name': '',
      'site.interval': '10s',
      'auth.public_read': false,
      'storage.max_backups': '3',
      'storage.retention': '2y',
    }
    expect(changesOf(before, after, settings)).toEqual({
      'site.name': null,
      'auth.public_read': false,
      'storage.max_backups': 3,
      'storage.retention': '2y',
    })
    expect(changesOf(before, before, settings)).toEqual({})
  })

  it('passes a number that does not parse on as text for the server to name', () => {
    const before = formOf(settings)
    expect(changesOf(before, { ...before, 'storage.max_backups': 'x' }, settings)).toEqual({
      'storage.max_backups': 'x',
    })
  })
})
