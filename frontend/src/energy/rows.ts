// Which energy series the overview shows for a site (spec 11.3, 11.4): pure, driven by
// the site model only.
import type { EnergyValue, SiteModel } from '@/api/types'
import { categoryToken } from '@/charts/tokens'

type RowColor = 'pv' | 'home' | 'grid' | 'battery' | 'ev' | 'heatpump' | 'heater' | 'other' | 'neutral'

export interface EnergyRow {
  name: string
  /** i18n key of the label; loadpoints use roles.<category> with a count. */
  labelKey: string
  color: RowColor
  ratio: boolean
}

const loadpointPrefix = 'site/loadpoints/'

/** Table rows in display order, only for what the site has. */
export function energyRows(site: SiteModel): EnergyRow[] {
  const has = new Set(site.site_series)
  const row = (name: string, labelKey: string, color: RowColor, ratio = false): EnergyRow => ({
    name,
    labelKey,
    color,
    ratio,
  })
  const out: EnergyRow[] = []
  if (has.has('site/pv')) out.push(row('site/pv', 'energy.pv', 'pv'))
  out.push(row('site/consumption', 'energy.consumption', 'home'))
  out.push(row('site/grid_in', 'energy.gridIn', 'grid'))
  out.push(row('site/grid_out', 'energy.gridOut', 'grid'))
  if (has.has('site/battery_in')) {
    out.push(row('site/battery_in', 'energy.batteryIn', 'battery'))
    out.push(row('site/battery_out', 'energy.batteryOut', 'battery'))
  }
  for (const name of site.site_series.filter((s) => s.startsWith(loadpointPrefix))) {
    const cat = name.slice(loadpointPrefix.length)
    out.push(row(name, `roles.${cat}`, categoryToken(cat)))
  }
  out.push(row('site/self_sufficiency', 'energy.selfSufficiency', 'neutral', true))
  if (has.has('site/pv')) {
    out.push(row('site/self_consumption', 'energy.selfConsumption', 'neutral', true))
  }
  return out
}

/** KPI tiles: PV, consumption, self-sufficiency, self-consumption, import, export. */
export function kpiRows(rows: EnergyRow[], phone: boolean): EnergyRow[] {
  const order = [
    'site/pv',
    'site/consumption',
    'site/self_sufficiency',
    'site/self_consumption',
    'site/grid_in',
    'site/grid_out',
  ]
  const picked = order
    .map((n) => rows.find((r) => r.name === n))
    .filter((r): r is EnergyRow => r !== undefined)
  if (!phone) return picked
  // Phone: four tiles, import instead of self-consumption.
  return picked.filter((r) => r.name !== 'site/self_consumption').slice(0, 4)
}

/** The number of a value: Wh or percent, null when missing. */
export function numberOf(v: EnergyValue | undefined): number | null {
  if (!v) return null
  return v.wh ?? v.pct ?? null
}
