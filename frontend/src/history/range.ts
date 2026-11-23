// History range (spec 11.8): granularity, presets, shifting and the periods of a range.
// Pure: "today" comes in as an argument, so it is tested without a clock.
import { addDays, addMonths, addYears, daysInMonth, type LocalDate } from '@/format/localdate'

export type Granularity = 'day' | 'month' | 'year'

export const granularities: Granularity[] = ['day', 'month', 'year']

/** Range lengths offered per granularity, in periods. */
export const presets: Record<Granularity, number[]> = {
  day: [7, 30, 90, 365],
  month: [6, 12, 24, 36],
  year: [3, 5, 10],
}

export const defaultLength: Record<Granularity, number> = { day: 30, month: 12, year: 5 }

/** n periods of a granularity ending with the period that contains `end`. */
export interface Range {
  gran: Granularity
  n: number
  /** Last day shown: today or the last day of a past period. */
  end: LocalDate
}

/** Moves a date by n periods. */
function step(gran: Granularity, d: LocalDate, n: number): LocalDate {
  if (gran === 'day') return addDays(d, n)
  return gran === 'month' ? addMonths(d, n) : addYears(d, n)
}

/** First day of the period containing d. */
function periodStart(gran: Granularity, d: LocalDate): LocalDate {
  if (gran === 'day') return d
  return gran === 'month' ? `${d.slice(0, 7)}-01` : `${d.slice(0, 4)}-01-01`
}

/** Last day of the period containing d. */
function periodEnd(gran: Granularity, d: LocalDate): LocalDate {
  if (gran === 'day') return d
  if (gran === 'year') return `${d.slice(0, 4)}-12-31`
  const days = daysInMonth(Number(d.slice(0, 4)), Number(d.slice(5, 7)))
  return `${d.slice(0, 7)}-${String(days).padStart(2, '0')}`
}

/** The API key of the period containing d (as GET /api/v1/energy returns it). */
export function periodKey(gran: Granularity, d: LocalDate): string {
  if (gran === 'day') return d
  return gran === 'month' ? d.slice(0, 7) : d.slice(0, 4)
}

/** First and last day of the range (from is the start of the first period). */
export function bounds(r: Range): { from: LocalDate; to: LocalDate } {
  return { from: periodStart(r.gran, step(r.gran, periodStart(r.gran, r.end), 1 - r.n)), to: r.end }
}

/** Start days of the range's periods, oldest first. */
export function periods(r: Range): LocalDate[] {
  const out: LocalDate[] = []
  for (let d = bounds(r).from, i = 0; i < r.n; d = step(r.gran, d, 1), i++) out.push(d)
  return out
}

/** The range ending today. */
export function latest(gran: Granularity, n: number, today: LocalDate): Range {
  return { gran, n, end: today }
}

/** Shifts the range by its length (dir -1 back, +1 forward); never past today. */
export function shift(r: Range, dir: -1 | 1, today: LocalDate): Range {
  const end = periodEnd(r.gran, step(r.gran, periodStart(r.gran, r.end), dir * r.n))
  return { ...r, end: end > today ? today : end }
}

/** Whether the range already ends today ("next" is disabled). */
export function atToday(r: Range, today: LocalDate): boolean {
  return r.end >= today
}

/**
 * The range covering the periods of `from` through `to` (in any order); it ends with the
 * period of `to`, never after today.
 */
export function customRange(gran: Granularity, from: LocalDate, to: LocalDate, today: LocalDate): Range {
  const [a, b] = from <= to ? [from, to] : [to, from]
  const end = periodLastDay(gran, periodStart(gran, b), today)
  let n = 0
  for (let d = periodStart(gran, a); d <= end; d = step(gran, d, 1)) n++
  return { gran, n, end }
}

/** Last day of the period that starts at `start`, never after today. */
export function periodLastDay(gran: Granularity, start: LocalDate, today: LocalDate): LocalDate {
  const end = periodEnd(gran, start)
  return end > today ? today : end
}
