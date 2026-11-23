// Status history (spec 11.3 Status, 12): the worst level of each day from stored changes,
// for the generic device level and for single registers alike. Pure; "at" is server
// time, so its date part is the local day.
import type { StatusEvent, StatusLevel } from '@/api/types'
import { addDays, midnightIn, type LocalDate } from '@/format/localdate'

/** A level since a time. */
export interface LevelAt {
  at: string
  level: StatusLevel
}

// "none" (pvmon stopped) loses against everything that was seen on the same day.
const rank: Record<StatusLevel, number> = { none: 0, ok: 1, notice: 2, offline: 3, fault: 4 }

/** The worse of two levels. */
export function worst(a: StatusLevel, b: StatusLevel): StatusLevel {
  return rank[b] > rank[a] ? b : a
}

/** CSS colour of a level; null (nothing known) uses the line colour like "none". */
export function levelColor(l: StatusLevel | null): string {
  switch (l) {
    case 'ok':
      return 'var(--battery)'
    case 'notice':
      return 'var(--notice)'
    case 'fault':
      return 'var(--fault)'
    case 'offline':
      return 'var(--ink-2)'
    default:
      return 'var(--line)'
  }
}

/** Days of a range of n days ending today, oldest first. */
export function statusDays(today: LocalDate, n: number): LocalDate[] {
  return Array.from({ length: n }, (_, i) => addDays(today, i + 1 - n))
}

/**
 * Worst level per day; null for days before the first known state. The changes are the
 * API answer: changes in the range plus the state active at its start, in any order.
 */
export function dayLevels(changes: LevelAt[], days: LocalDate[]): (StatusLevel | null)[] {
  const sorted = [...changes].sort((a, b) => a.at.localeCompare(b.at))
  let i = 0
  let current: StatusLevel | null = null
  return days.map((day) => {
    while (i < sorted.length && sorted[i]!.at.slice(0, 10) < day) current = sorted[i++]!.level
    let level = current
    while (i < sorted.length && sorted[i]!.at.slice(0, 10) === day) {
      current = sorted[i++]!.level
      level = level === null ? current : worst(level, current)
    }
    return level
  })
}

/** dayLevels per register. */
export function registerDays(
  events: StatusEvent[],
  registers: string[],
  days: LocalDate[],
): Record<string, (StatusLevel | null)[]> {
  return Object.fromEntries(
    registers.map((r) => [r, dayLevels(events.filter((e) => e.register === r), days)]),
  )
}

/** The newest change (null when nothing is stored). */
export function newest<T extends LevelAt>(changes: T[]): T | null {
  let out: T | null = null
  for (const c of changes) if (!out || c.at > out.at) out = c
  return out
}

/** A state of a day with how long it lasted: the generic level, or a register message. */
export interface DaySpan {
  key: string
  level: StatusLevel
  ms: number
}

/** A change with the key it is shown under (the level itself, or a message). */
export interface KeyedChange extends LevelAt {
  key: string
}

/**
 * How long each state lasted on each day (longest first), up to `now` for today; empty
 * before the first known state. Day bounds are local midnights of the site timezone.
 */
export function daySpans(
  changes: KeyedChange[],
  days: LocalDate[],
  timeZone: string,
  now: number,
): DaySpan[][] {
  const sorted = changes
    .map((c) => ({ ...c, t: Date.parse(c.at) }))
    .sort((a, b) => a.t - b.t)
  let i = 0
  let cur: (typeof sorted)[number] | null = null
  return days.map((day) => {
    const start = midnightIn(day, timeZone)
    const end = Math.min(midnightIn(addDays(day, 1), timeZone), now)
    const spans = new Map<string, DaySpan>()
    const add = (c: KeyedChange | null, from: number, to: number) => {
      if (!c || to <= from) return
      const s = spans.get(c.key) ?? { key: c.key, level: c.level, ms: 0 }
      s.ms += to - from
      spans.set(c.key, s)
    }
    while (i < sorted.length && sorted[i]!.t <= start) cur = sorted[i++]!
    let from = start
    while (i < sorted.length && sorted[i]!.t < end) {
      add(cur, from, sorted[i]!.t)
      cur = sorted[i++]!
      from = cur.t
    }
    add(cur, from, end)
    return [...spans.values()].sort((a, b) => b.ms - a.ms)
  })
}
