// Report periods (spec 12): local dates of the server time zone, to ≤ today. Pure.
import type { ReportQuery } from '@/api/client'
import {
  addDays,
  addMonths,
  parseLocalDate,
  startOfMonth,
  type LocalDate,
} from '@/format/localdate'

export const presets = ['last_month', 'this_month', 'last_year', 'this_year', 'last_30'] as const
export type Preset = (typeof presets)[number]
export const compares = ['last_year', 'previous', 'none'] as const
export type Compare = (typeof compares)[number]

/** The days of a preset relative to today (a range ending today ends yesterday server-side). */
export function presetRange(p: Preset, today: LocalDate): { from: LocalDate; to: LocalDate } {
  const year = today.slice(0, 4)
  switch (p) {
    case 'this_month':
      return { from: startOfMonth(today), to: today }
    case 'last_year':
      return {
        from: `${Number(year) - 1}-01-01`,
        to: `${Number(year) - 1}-12-31`,
      }
    case 'this_year':
      return { from: `${year}-01-01`, to: today }
    case 'last_30':
      return { from: addDays(today, -30), to: addDays(today, -1) }
    default: {
      const first = addMonths(startOfMonth(today), -1)
      return { from: first, to: addDays(startOfMonth(today), -1) }
    }
  }
}

/** The preset whose days equal from..to, if any. */
export function presetOf(from: string, to: string, today: LocalDate): Preset | null {
  return (
    presets.find((p) => {
      const r = presetRange(p, today)
      return r.from === from && r.to === to
    }) ?? null
  )
}

/** The query from route parameters; invalid or missing values fall back to last month. */
export function reportQuery(q: Record<string, unknown>, today: LocalDate): ReportQuery {
  const str = (v: unknown) => (typeof v === 'string' ? v : '')
  const fallback = presetRange('last_month', today)
  let from = str(q.from)
  let to = str(q.to)
  if (!parseLocalDate(from) || !parseLocalDate(to) || from > to || to > today) {
    ;({ from, to } = fallback)
  }
  const c = str(q.compare)
  const compare = (compares as readonly string[]).includes(c) ? (c as Compare) : 'last_year'
  return { from, to, compare }
}
