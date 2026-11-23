// SiteModel fixtures for the reference setups (spec 2), shaped like GET /api/v1/site.
import type { SiteModel } from '@/api/types'

type Meter = SiteModel['meters'][number]

function meter(m: Omit<Meter, 'label' | 'device' | 'series'> & { series?: string[] }): Meter {
  return { label: m.id, device: m.id, series: [`${m.id}/power`], ...m }
}

function site(parts: Partial<SiteModel> & Pick<SiteModel, 'meters' | 'capabilities'>): SiteModel {
  return {
    name: 'Test',
    tz: 'Europe/Berlin',
    config_rev: 1,
    data_since: null,
    devices: [],
    site_series: [],
    ...parts,
  }
}

const solisMeters = [
  meter({ id: 'pv1', role: 'pv' }),
  meter({ id: 'pv2', role: 'pv' }),
  meter({ id: 'bat', role: 'battery', capacity_wh: 10000 }),
  meter({ id: 'backup', role: 'backup', parent: 'inverter' }),
]

const baseSeries = [
  'site/pv',
  'site/grid_in',
  'site/grid_out',
  'site/battery_in',
  'site/battery_out',
  'site/soc',
  'site/consumption',
  'site/house_rest',
]

/** R1: Solis hybrid, grid measured on the inverter. */
export const r1 = site({
  capabilities: { pv: 2, battery: 1, grid: true, backup: true, loadpoints: {} },
  meters: [...solisMeters, meter({ id: 'grid', role: 'grid' })],
  site_series: baseSeries,
})

/** R3: R1 plus a Shelly grid meter; the inverter grid becomes role inverter. */
export const r3 = site({
  capabilities: { pv: 2, battery: 1, grid: true, backup: true, loadpoints: {} },
  meters: [
    ...solisMeters,
    meter({ id: 'inv_grid', role: 'inverter' }),
    meter({ id: 'shelly', role: 'grid' }),
  ],
  site_series: baseSeries,
})

/** R4: R3 plus two wallboxes directly under the site. */
export const r4 = site({
  capabilities: { pv: 2, battery: 1, grid: true, backup: true, loadpoints: { wallbox: 2 } },
  meters: [
    ...r3.meters,
    meter({ id: 'goe', role: 'consumer', category: 'wallbox', parent: 'site' }),
    meter({ id: 'warp', role: 'consumer', category: 'wallbox', parent: 'site' }),
  ],
  site_series: [...baseSeries, 'site/loadpoints/wallbox'],
})

/** Minimal PV plus grid. */
export const pvGrid = site({
  capabilities: { pv: 1, battery: 0, grid: true, backup: false, loadpoints: {} },
  meters: [meter({ id: 'pv1', role: 'pv' }), meter({ id: 'grid', role: 'grid' })],
  site_series: ['site/pv', 'site/grid_in', 'site/grid_out', 'site/consumption', 'site/house_rest'],
})

/** Grid meter only. */
export const gridOnly = site({
  capabilities: { pv: 0, battery: 0, grid: true, backup: false, loadpoints: {} },
  meters: [meter({ id: 'grid', role: 'grid' })],
  site_series: ['site/grid_in', 'site/grid_out', 'site/consumption', 'site/house_rest'],
})
