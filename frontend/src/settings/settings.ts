import type { SettingView } from '@/api/types'

/** The settings the UI may change (spec 3), in display order, with their input kind. */
export const settingFields = [
  { key: 'site.name', kind: 'text' },
  { key: 'site.interval', kind: 'duration' },
  { key: 'status.offline_after', kind: 'duration' },
  { key: 'auth.public_read', kind: 'bool' },
  { key: 'storage.retention', kind: 'duration' },
  { key: 'storage.backup_interval', kind: 'duration' },
  { key: 'storage.max_backups', kind: 'number' },
] as const

export type SettingKey = (typeof settingFields)[number]['key']

/** Form values: text inputs hold strings, the switch a boolean. */
export type SettingForm = Record<SettingKey, string | boolean>

export function formOf(settings: Record<string, SettingView>): SettingForm {
  const out = {} as SettingForm
  for (const f of settingFields) {
    const v = settings[f.key]?.value
    out[f.key] = f.kind === 'bool' ? v === true : v === undefined || v === null ? '' : String(v)
  }
  return out
}

/**
 * The changed, unlocked keys with their typed values; an emptied stored field resets to
 * the default (null). A number that does not parse is sent as text so the server names it.
 */
export function changesOf(
  before: SettingForm,
  after: SettingForm,
  settings: Record<string, SettingView>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of settingFields) {
    const a = after[f.key]
    if (settings[f.key]?.locked || a === before[f.key]) continue
    if (f.kind === 'bool') out[f.key] = a
    else if (a === '') out[f.key] = null
    else if (f.kind === 'number') out[f.key] = Number.isFinite(Number(a)) ? Number(a) : a
    else out[f.key] = a
  }
  return out
}
