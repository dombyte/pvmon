// Month grid of the date picker. Pure: weeks start on Monday (de and en UI alike, as the
// rest of the app), dates are local YYYY-MM-DD strings.
import { addDays, addMonths, type LocalDate } from '@/format/localdate'

/** One day cell of the grid. */
export interface Cell {
  date: LocalDate
  /** The day belongs to the shown month (others are shown dimmed). */
  inMonth: boolean
}

const weekDays = 7
const gridWeeks = 6

/** Weekday of a date, 0 = Monday. */
function weekday(date: LocalDate): number {
  const [y, m, d] = date.split('-').map(Number)
  return (new Date(Date.UTC(y!, m! - 1, d!)).getUTCDay() + weekDays - 1) % weekDays
}

/** The 6 × 7 days shown for the month of `month` (any day of it). */
export function monthGrid(month: LocalDate): Cell[] {
  const first = `${month.slice(0, 7)}-01`
  const start = addDays(first, -weekday(first))
  return Array.from({ length: gridWeeks * weekDays }, (_, i) => {
    const date = addDays(start, i)
    return { date, inMonth: date.slice(0, 7) === first.slice(0, 7) }
  })
}

/** A Monday, for the weekday header (2024-01-01 was one). */
export const mondayRef: LocalDate = '2024-01-01'

/** The first day of the month n months away. */
export function shiftMonth(month: LocalDate, n: number): LocalDate {
  return addMonths(`${month.slice(0, 7)}-01`, n)
}

/** Whether a date lies within [min, max] (empty bounds are open). */
export function selectable(date: LocalDate, min?: LocalDate, max?: LocalDate): boolean {
  return (!min || date >= min) && (!max || date <= max)
}

/** The date clamped into [min, max]. */
export function clampDate(date: LocalDate, min?: LocalDate, max?: LocalDate): LocalDate {
  if (min && date < min) return min
  if (max && date > max) return max
  return date
}

/** What a picker chooses: a day, or a whole month or year (given as its first day). */
export type Unit = 'day' | 'month' | 'year'

const monthsPerYear = 12
/** Years shown per page of the year picker (a 3 × 4 grid like the months). */
export const yearsPerPage = 12

/** First day of the period of `unit` that holds `date`. */
export function periodOf(unit: Unit, date: LocalDate): LocalDate {
  if (unit === 'month') return `${date.slice(0, 7)}-01`
  if (unit === 'year') return `${date.slice(0, 4)}-01-01`
  return date
}

/** The month or year cells of one page: the 12 months of `date`'s year, or 12 years. */
export function periodCells(unit: 'month' | 'year', date: LocalDate): LocalDate[] {
  const year = Number(date.slice(0, 4))
  if (unit === 'month') {
    return Array.from({ length: monthsPerYear }, (_, i) =>
      `${year}-${String(i + 1).padStart(2, '0')}-01`,
    )
  }
  const first = Math.floor(year / yearsPerPage) * yearsPerPage
  return Array.from({ length: yearsPerPage }, (_, i) => `${first + i}-01-01`)
}

/** Whether a period overlaps [min, max] (both compared at the period's own precision). */
export function periodSelectable(
  unit: Unit,
  date: LocalDate,
  min?: LocalDate,
  max?: LocalDate,
): boolean {
  if (unit === 'day') return selectable(date, min, max)
  const len = unit === 'month' ? 7 : 4
  const p = date.slice(0, len)
  return (!min || p >= min.slice(0, len)) && (!max || p <= max.slice(0, len))
}

/** The period `n` steps away (months or years). */
export function shiftPeriod(unit: 'month' | 'year', date: LocalDate, n: number): LocalDate {
  return unit === 'month' ? shiftMonth(date, n) : `${Number(date.slice(0, 4)) + n}-01-01`
}
