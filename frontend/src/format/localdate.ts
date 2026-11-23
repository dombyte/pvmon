// Local calendar dates (YYYY-MM-DD) without a date library. Arithmetic runs on UTC
// midnights, so DST never shifts a day; the server timezone only matters for "today".

export type LocalDate = string

const DATE_RE = /^(\d{4})-(\d{2})-(\d{2})$/

function pad(n: number, width: number): string {
  return String(n).padStart(width, '0')
}

/** Parses a local date; returns null for anything that is not a real calendar date. */
export function parseLocalDate(s: string): { y: number; m: number; d: number } | null {
  const match = DATE_RE.exec(s)
  if (!match) return null
  const y = Number(match[1])
  const m = Number(match[2])
  const d = Number(match[3])
  const probe = new Date(Date.UTC(y, m - 1, d))
  if (probe.getUTCFullYear() !== y || probe.getUTCMonth() !== m - 1 || probe.getUTCDate() !== d) {
    return null
  }
  return { y, m, d }
}

function fromUTC(t: Date): LocalDate {
  return `${pad(t.getUTCFullYear(), 4)}-${pad(t.getUTCMonth() + 1, 2)}-${pad(t.getUTCDate(), 2)}`
}

function toUTC(date: LocalDate): Date {
  const p = parseLocalDate(date)
  if (!p) throw new Error(`invalid local date: ${date}`)
  return new Date(Date.UTC(p.y, p.m - 1, p.d))
}

export function addDays(date: LocalDate, n: number): LocalDate {
  const t = toUTC(date)
  t.setUTCDate(t.getUTCDate() + n)
  return fromUTC(t)
}

/** Adds months and clamps the day to the target month's length (31 Jan + 1 = 28/29 Feb). */
export function addMonths(date: LocalDate, n: number): LocalDate {
  const p = parseLocalDate(date)
  if (!p) throw new Error(`invalid local date: ${date}`)
  const first = new Date(Date.UTC(p.y, p.m - 1 + n, 1))
  const last = daysInMonth(first.getUTCFullYear(), first.getUTCMonth() + 1)
  first.setUTCDate(Math.min(p.d, last))
  return fromUTC(first)
}

export function addYears(date: LocalDate, n: number): LocalDate {
  return addMonths(date, n * 12)
}

export function daysInMonth(year: number, month: number): number {
  return new Date(Date.UTC(year, month, 0)).getUTCDate()
}

export function startOfMonth(date: LocalDate): LocalDate {
  return `${date.slice(0, 7)}-01`
}

/** Whole days from a to b (negative when b is earlier). */
export function diffDays(a: LocalDate, b: LocalDate): number {
  const msPerDay = 86_400_000
  return Math.round((toUTC(b).getTime() - toUTC(a).getTime()) / msPerDay)
}

/** The calendar date of an instant in a timezone (the site's, not the browser's). */
export function todayIn(timeZone: string, now: Date = new Date()): LocalDate {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(now)
  return parts
}

/** Offset of a timezone from UTC at an instant, in ms (wall clock − UTC). */
function offsetAt(timeZone: string, ms: number): number {
  const p = Object.fromEntries(
    new Intl.DateTimeFormat('en-US', {
      timeZone,
      hourCycle: 'h23',
      year: 'numeric',
      month: 'numeric',
      day: 'numeric',
      hour: 'numeric',
      minute: 'numeric',
      second: 'numeric',
    })
      .formatToParts(new Date(ms))
      .map((x) => [x.type, Number(x.value)]),
  ) as Record<string, number>
  const wall = Date.UTC(p.year!, p.month! - 1, p.day!, p.hour!, p.minute!, p.second!)
  return wall - Math.floor(ms / 1000) * 1000
}

/** The instant (ms) of local midnight starting a date in a timezone, DST included. */
export function midnightIn(date: LocalDate, timeZone: string): number {
  const wall = toUTC(date).getTime()
  const guess = wall - offsetAt(timeZone, wall)
  return wall - offsetAt(timeZone, guess)
}
