// Numbers and dates for the UI (spec 11.10): Intl with the locale of the chosen
// language, never the browser region.
import { parseLocalDate } from './localdate'

export type Lang = 'de' | 'en'

const NBSP = ' '
const WATTS_PER_KW = 1000
const KW_DECIMALS = 1
const PERCENT_DECIMALS = 0

const locales: Record<Lang, string> = { de: 'de-DE', en: 'en-GB' }
// en-GB would give "4,512.3"; use en-US number rules for English separators.
const numberLocales: Record<Lang, string> = { de: 'de-DE', en: 'en-US' }

export const missing = '–'

function nf(lang: Lang, digits: number, minDigits = digits): Intl.NumberFormat {
  return new Intl.NumberFormat(numberLocales[lang], {
    minimumFractionDigits: minDigits,
    maximumFractionDigits: digits,
    // "-0,0" for values that round to zero would read as a negative reading.
    signDisplay: 'negative',
  })
}

/** Plain number with at most `digits` decimals (at least `minDigits`); non-finite = missing. */
export function formatNumber(
  value: number | null | undefined,
  lang: Lang,
  digits = 0,
  minDigits = digits,
): string {
  if (value == null || !Number.isFinite(value)) return missing
  return nf(lang, digits, minDigits).format(value)
}

/** Power: below 1 kW whole watts, otherwise kW with one decimal. */
export function formatPower(watts: number | null | undefined, lang: Lang): string {
  if (watts == null || !Number.isFinite(watts)) return missing
  if (Math.abs(watts) < WATTS_PER_KW) return `${nf(lang, 0).format(watts)}${NBSP}W`
  return `${nf(lang, KW_DECIMALS).format(watts / WATTS_PER_KW)}${NBSP}kW`
}

/** Energy: always kWh with one decimal (stored as Wh). */
export function formatEnergy(wh: number | null | undefined, lang: Lang): string {
  if (wh == null || !Number.isFinite(wh)) return missing
  return `${nf(lang, KW_DECIMALS).format(wh / WATTS_PER_KW)}${NBSP}kWh`
}

/** Percent from a value in percent (0..100); reports use one decimal (spec 12). */
export function formatPercent(
  value: number | null | undefined,
  lang: Lang,
  digits = PERCENT_DECIMALS,
): string {
  if (value == null || !Number.isFinite(value)) return missing
  return `${nf(lang, digits).format(value)}${NBSP}%`
}

/** A local date (YYYY-MM-DD) as a calendar date; no timezone shift is possible. */
export function formatLocalDate(
  date: string,
  lang: Lang,
  options: Intl.DateTimeFormatOptions = { dateStyle: 'medium' },
): string {
  const p = parseLocalDate(date)
  if (!p) return missing
  return new Intl.DateTimeFormat(locales[lang], { ...options, timeZone: 'UTC' }).format(
    new Date(Date.UTC(p.y, p.m - 1, p.d)),
  )
}

/** An instant, shown in the site's timezone. */
export function formatDateTime(
  instant: Date | string,
  lang: Lang,
  timeZone: string,
  options: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'short' },
): string {
  const d = typeof instant === 'string' ? new Date(instant) : instant
  if (Number.isNaN(d.getTime())) return missing
  return new Intl.DateTimeFormat(locales[lang], { ...options, timeZone }).format(d)
}

const msPerMinute = 60_000
const minutesPerHour = 60

/** A duration as "3 h 12 min", "45 min" or "< 1 min". */
export function formatDuration(ms: number, lang: Lang): string {
  if (ms < msPerMinute) return `< 1${NBSP}min`
  const minutes = Math.round(ms / msPerMinute)
  const h = Math.floor(minutes / minutesPerHour)
  const m = minutes % minutesPerHour
  const hours = `${nf(lang, 0).format(h)}${NBSP}h`
  if (h === 0) return `${m}${NBSP}min`
  return m === 0 ? hours : `${hours} ${m}${NBSP}min`
}
