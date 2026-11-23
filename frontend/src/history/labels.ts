// Localised labels of history periods and ranges.
import { formatLocalDate, type Lang } from '@/format'
import type { LocalDate } from '@/format/localdate'
import { bounds, type Granularity, type Range } from './range'

/** Short axis label of a period. */
export function axisLabel(gran: Granularity, start: LocalDate, lang: Lang): string {
  if (gran === 'day') return formatLocalDate(start, lang, { day: 'numeric' })
  if (gran === 'month') return formatLocalDate(start, lang, { month: 'short' })
  return start.slice(0, 4)
}

/** Full name of a period ("18. Nov. 2026", "November 2026", "2026"). */
export function periodTitle(gran: Granularity, start: LocalDate, lang: Lang): string {
  if (gran === 'day') return formatLocalDate(start, lang, { day: 'numeric', month: 'short', year: 'numeric' })
  if (gran === 'month') return formatLocalDate(start, lang, { month: 'long', year: 'numeric' })
  return start.slice(0, 4)
}

/** The range button text ("21. Okt. – 19. Nov. 2026", "Dez. 2025 – Nov. 2026", "2022 – 2026"). */
export function rangeLabel(r: Range, lang: Lang): string {
  const { from, to } = bounds(r)
  if (r.gran === 'year') return `${from.slice(0, 4)} – ${to.slice(0, 4)}`
  if (r.gran === 'month') {
    const m = (d: LocalDate) => formatLocalDate(d, lang, { month: 'short', year: 'numeric' })
    return `${m(from)} – ${m(to)}`
  }
  const sameYear = from.slice(0, 4) === to.slice(0, 4)
  const start = formatLocalDate(from, lang, {
    day: 'numeric',
    month: 'short',
    ...(sameYear ? {} : { year: 'numeric' }),
  })
  return `${start} – ${formatLocalDate(to, lang, { day: 'numeric', month: 'short', year: 'numeric' })}`
}
