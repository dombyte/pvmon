// Formatting of live device channels by unit.
import { formatEnergy, formatNumber, formatPercent, formatPower, missing, type Lang } from '@/format'

const NBSP = ' '
const decimals: Record<string, number> = { V: 1, A: 2, Hz: 2, '°C': 1 }
const whPerKwh = 1000

/** A live value with its unit; strings (enums) are shown as they are. */
export function formatChannel(v: number | string | undefined, unit: string | undefined, lang: Lang): string {
  if (v === undefined) return missing
  if (typeof v === 'string') return v
  switch (unit) {
    case 'W':
      return formatPower(v, lang)
    case 'kW':
      return formatPower(v * whPerKwh, lang)
    case 'Wh':
      return formatEnergy(v, lang)
    case 'kWh':
      return formatEnergy(v * whPerKwh, lang)
    case '%':
      return formatPercent(v, lang)
    case undefined:
    case '':
      return formatNumber(v, lang, 2, 0)
    default:
      return `${formatNumber(v, lang, decimals[unit] ?? 2, 0)}${NBSP}${unit}`
  }
}
