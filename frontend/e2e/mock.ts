// A deterministic stand-in for the pvmon server: site model, energy, version and the
// WebSocket, all answered inside the browser context. "Now" is fixed.
import type { BrowserContext, Page } from '@playwright/test'
import type { EnergySeries, EnergyValue, Me, SiteModel } from '../src/api/types'
import { mockAdmin } from './admin'
import { reportOf } from './report'

const now = new Date('2026-11-19T13:42:05+01:00')
const today = '2026-11-19'
/** A day without data, to show the empty outline in the trend. */
const gapDay = '2026-11-12'

const live: Record<string, number> = {
  'site/pv.power': 2410,
  'site/grid_in.power': 1890,
  'site/grid_out.power': 0,
  'site/battery_in.power': 0,
  'site/battery_out.power': 1200,
  'site/consumption.power': 5500,
  'site/loadpoints/wallbox.power': 3680,
  'site/loadpoints/heatpump.power': 1200,
  'site/soc': 64,
  'pv1/power': 1400,
  'pv2/power': 1010,
  'bat/power': 1200,
  'grid/power': 690,
  'shelly/power': 690,
  'inv_grid/power': 690,
  'backup/power': 120,
  'goe/power': 3680,
  'warp/power': 0,
  // Meter IDs of the Go reference setups (internal/simulate).
  'pv/power': 2410,
  'pv-mppt1/power': 1400,
  'pv-mppt2/power': 1010,
  'battery/power': 1200,
  'inv-grid/power': 690,
  'wb-garage/power': 3680,
  'wb-drive/power': 0,
  'heatpump/power': 1200,
}

// Daily Wh per series; the day number varies them so the bars differ.
const daily: Record<string, (n: number) => number> = {
  'site/pv': (n) => 3000 + ((n * 37) % 9) * 900,
  'site/consumption': (n) => 9500 + ((n * 53) % 7) * 700,
  'site/grid_in': (n) => 4000 + ((n * 29) % 5) * 500,
  'site/grid_out': (n) => 300 + ((n * 17) % 4) * 200,
  'site/battery_in': (n) => 1800 + ((n * 11) % 3) * 300,
  'site/battery_out': (n) => 1600 + ((n * 13) % 3) * 300,
  'site/loadpoints/wallbox': (n) => 3900 + ((n * 7) % 4) * 400,
  'site/loadpoints/heatpump': (n) => 4800 + ((n * 5) % 3) * 600,
  'site/self_sufficiency': (n) => 45 + ((n * 19) % 6) * 5,
  'site/self_consumption': (n) => 80 + ((n * 23) % 4) * 4,
}
daily['site/house_rest'] = (n) =>
  daily['site/consumption']!(n) -
  daily['site/loadpoints/wallbox']!(n) -
  daily['site/loadpoints/heatpump']!(n)
const perLevel: Record<string, number> = { month: 19, year: 323, total: 628 }

function value(name: string, period: string, n: number, factor = 1): EnergyValue {
  const ratio = name.endsWith('self_sufficiency') || name.endsWith('self_consumption')
  const missing = period === gapDay
  const raw = missing ? null : (daily[name]?.(n) ?? 0) * (ratio ? 1 : factor)
  return {
    period,
    ...(ratio ? { pct: raw } : { wh: raw }),
    complete: !missing && period !== today,
    estimated: false,
    missing_days: missing ? 1 : 0,
    missing: missing ? [period] : [],
  }
}

function days(from: string, to: string): string[] {
  const out: string[] = []
  for (let t = Date.parse(`${from}T00:00:00Z`); t <= Date.parse(`${to}T00:00:00Z`); t += 864e5) {
    out.push(new Date(t).toISOString().slice(0, 10))
  }
  return out
}

// 15-minute model of a day in W, by hour of day (h = 0..24).
const slotPower: Record<string, (h: number) => number> = {
  'site/pv': (h) => Math.max(0, Math.sin(((h - 7.5) / 9) * Math.PI)) * 2600,
  'site/loadpoints/wallbox': (h) => (h >= 13 && h < 15 ? 3680 : 0),
  'site/loadpoints/heatpump': (h) => (h >= 6 && h < 7) || (h >= 20 && h < 21.5) ? 1200 : 0,
}
function slotValues(h: number): Record<string, number> {
  const pv = slotPower['site/pv']!(h)
  const wb = slotPower['site/loadpoints/wallbox']!(h)
  const hp = slotPower['site/loadpoints/heatpump']!(h)
  const cons = 350 + wb + hp + (h >= 18 && h < 22 ? 600 : 0)
  const surplus = pv - cons
  const battery = Math.max(-1200, Math.min(1200, -surplus)) // + discharge
  const grid = cons - pv - battery
  return {
    'site/pv': pv,
    'site/consumption': cons,
    'site/house_rest': cons - wb - hp,
    'site/loadpoints/wallbox': wb,
    'site/loadpoints/heatpump': hp,
    'site/grid_in': Math.max(grid, 0),
    'site/grid_out': Math.max(-grid, 0),
    'site/battery_out': Math.max(battery, 0),
    'site/battery_in': Math.max(-battery, 0),
  }
}
const pad2 = (n: number) => String(n).padStart(2, '0')
/** Slot starts of a November day (UTC+1) and whether they lie before "now". */
function slotsOf(day: string): { at: string; h: number; past: boolean }[] {
  return Array.from({ length: 96 }, (_, i) => {
    const at = `${day}T${pad2(Math.floor(i / 4))}:${pad2((i % 4) * 15)}:00+01:00`
    return { at, h: i / 4, past: Date.parse(at) + 15 * 60_000 <= now.getTime() }
  })
}

function gauge(url: URL): { series: { name: string; unit: string; points: object[] }[] } {
  const names = (url.searchParams.get('series') ?? '').split(',')
  const day = url.searchParams.get('from') ?? today
  return {
    series: names.map((name) => ({
      name,
      unit: name === 'site/soc' ? '%' : 'W',
      points: slotsOf(day)
        .filter((s) => s.past)
        .map(({ at, h }) => {
          const avg = name === 'site/soc' ? 20 + 60 * Math.max(0, Math.sin(((h - 9) / 14) * Math.PI)) : slotPower['site/pv']!(h)
          return { at, avg, min: avg * 0.8, max: avg * 1.15, cover: 900 }
        }),
    })),
  }
}

function energy(url: URL): { series: EnergySeries[] } {
  const names = (url.searchParams.get('series') ?? '').split(',')
  const level = url.searchParams.get('level') ?? 'day'
  const from = url.searchParams.get('from') ?? today
  const to = url.searchParams.get('to') ?? today
  const series = names.map((name): EnergySeries => {
    const unit = name.includes('self_') ? '%' : 'Wh'
    if (level === 'bucket') {
      return {
        name,
        unit,
        values: slotsOf(from).map(({ at, h, past }) => ({
          period: at,
          wh: past ? (slotValues(h)[name] ?? 0) / 4 : null,
          complete: past,
          estimated: false,
          missing_days: 0,
          missing: [],
        })),
      }
    }
    if (level === 'day') {
      return { name, unit, values: days(from, to).map((d) => value(name, d, Number(d.slice(8)))) }
    }
    const period = level === 'month' ? today.slice(0, 7) : level === 'year' ? today.slice(0, 4) : 'total'
    return { name, unit, values: [value(name, period, 3, perLevel[level] ?? 1)] }
  })
  return { series }
}

/** Live values of a site: the house rest is what its loadpoints leave of consumption. */
function liveOf(site: SiteModel): Record<string, number> {
  let rest = live['site/consumption.power'] ?? 0
  for (const s of site.site_series) {
    if (s.startsWith('site/loadpoints/')) rest -= live[`${s}.power`] ?? 0
  }
  return { ...live, 'site/house_rest.power': rest }
}

/** Device list and details derived from the site's meters (no real profile data). */
function devicesOf(site: SiteModel) {
  const ids = [...new Set(site.meters.map((m) => m.device))].sort()
  const info = (id: string) => ({
    id,
    profile: site.devices.find((d) => d.id === id)?.profile ?? 'test/profile',
    vendor: id === 'wr1' ? 'Solis' : 'Vendor',
    models: [id === 'wr1' ? 'S6-EH3P' : 'Model'],
    transport: id === 'wr1' ? 'modbus' : 'http',
    verified: id !== 'goe',
    state: id === 'warp' ? 'offline' : 'online',
    interval_s: 5,
    meters: site.meters.filter((m) => m.device === id).map((m) => m.id),
    last_poll:
      id === 'warp'
        ? { at: '2026-11-19T13:42:00+01:00', took_ms: 2003, ok: false }
        : { at: '2026-11-19T13:42:03+01:00', took_ms: 312, ok: true },
  })
  const detail = (id: string) => ({
    ...info(id),
    read: id === 'wr1' ? 'range' : undefined,
    status:
      id === 'wr1'
        ? [
            { name: 'grid_fault', label: { de: 'Netzfehler', en: 'Grid faults' } },
            { name: 'operating', label: { de: 'Betriebszustand', en: 'Operating state' } },
          ]
        : [{ name: 'errors' }],
    blocks: site.meters
      .filter((m) => m.device === id)
      .map((m) => ({
        meter: m.id,
        label: m.label,
        role: m.role,
        block: m.id,
        channels: m.series.map((k) => ({ name: k.split('/')[1], key: k, unit: k.endsWith('power') ? 'W' : undefined })),
      })),
  })
  const events = (id: string) =>
    id === 'wr1'
      ? [
          { register: 'operating', at: '2026-11-19T07:12:00+01:00', code: 3, level: 'ok', texts: [{ de: 'Netzbetrieb', en: 'Generating' }] },
          { register: 'operating', at: '2026-11-12T03:00:00+01:00', code: 0, level: 'notice', texts: [{ de: 'Wartend', en: 'Waiting' }] },
          { register: 'grid_fault', at: '2026-11-05T14:20:00+01:00', code: 0, level: 'ok', texts: [] },
          { register: 'grid_fault', at: '2026-11-05T14:02:00+01:00', code: 4, level: 'fault', texts: [{ de: 'Netzüberspannung', en: 'Grid overvoltage' }] },
          { register: 'operating', at: '2026-10-01T00:00:00+01:00', code: 3, level: 'ok', texts: [{ de: 'Netzbetrieb', en: 'Generating' }] },
          { register: 'grid_fault', at: '2026-10-01T00:00:00+01:00', code: 0, level: 'ok', texts: [] },
          { register: '@connection', at: '2026-11-10T09:30:00+01:00', code: 0, level: 'ok', texts: null },
          { register: '@connection', at: '2026-11-10T08:00:00+01:00', code: 2, level: 'none', texts: null },
          { register: '@connection', at: '2026-10-01T00:00:00+01:00', code: 0, level: 'ok', texts: null },
        ]
      : [
          { register: 'errors', at: '2026-10-01T00:00:00+01:00', code: 0, level: 'ok', texts: [] },
          { register: '@connection', at: '2026-10-01T00:00:00+01:00', code: 0, level: 'ok', texts: null },
          ...(id === 'warp'
            ? [{ register: '@connection', at: '2026-11-18T22:10:00+01:00', code: 1, level: 'offline', texts: null }]
            : []),
        ]
  /** The generic level per device, as GET /api/v1/status answers it. */
  const overview = (id: string) =>
    id === 'wr1'
      ? [
          { at: '2026-10-01T00:00:00+01:00', level: 'ok' },
          { at: '2026-11-05T14:02:00+01:00', level: 'fault' },
          { at: '2026-11-05T14:20:00+01:00', level: 'ok' },
          { at: '2026-11-10T08:00:00+01:00', level: 'none' },
          { at: '2026-11-10T09:30:00+01:00', level: 'ok' },
          { at: '2026-11-12T03:00:00+01:00', level: 'notice' },
          { at: '2026-11-19T07:12:00+01:00', level: 'ok' },
        ]
      : [
          { at: '2026-10-01T00:00:00+01:00', level: 'ok' },
          ...(id === 'warp' ? [{ at: '2026-11-18T22:10:00+01:00', level: 'offline' }] : []),
        ]
  return { ids, info, detail, events, overview }
}

/** The test's built UI version; /api/v1/version answers it unless `version` is given. */
export const uiVersion = '1.0.0-e2e'

/**
 * Serves `site` and all other API answers for one page. With a service worker the routes
 * go on the context (`target`): requests the worker makes pass page.route by.
 */
export async function mockServer(
  page: Page,
  site: SiteModel,
  opts: { target?: BrowserContext; version?: () => string; me?: Me } = {},
): Promise<void> {
  const target = opts.target ?? page
  const version = opts.version ?? (() => uiVersion)
  await page.clock.setFixedTime(now)
  const json = (body: unknown) => ({ contentType: 'application/json', body: JSON.stringify(body) })
  // Anything not answered below is a test bug: fail loudly instead of hitting the proxy.
  await target.route('**/api/**', (r) => {
    console.error(`unmocked request: ${r.request().url()}`)
    return r.fulfill({ status: 599 })
  })
  await target.route('**/api/v1/site', (r) => r.fulfill(json(site)))
  const me = opts.me ?? { user: null, setup_required: false, public_read: true }
  await target.route('**/api/v1/auth/me', (r) => r.fulfill(json(me)))
  await mockAdmin(target)
  await target.route('**/api/v1/version', (r) =>
    r.fulfill(json({ version: version(), commit: 'e2e', build_date: '', go: '' })),
  )
  await target.route((u) => u.pathname === '/api/v1/energy', (r) => r.fulfill(json(energy(new URL(r.request().url())))))
  await target.route((u) => u.pathname === '/api/v1/gauge', (r) => r.fulfill(json(gauge(new URL(r.request().url())))))
  await target.route(
    (u) => u.pathname === '/api/v1/report',
    (r) => {
      const url = new URL(r.request().url())
      const format = url.searchParams.get('format')
      if (format === 'pdf') return r.fulfill({ status: 500, ...json({ code: 'internal' }) })
      if (format === 'csv')
        return r.fulfill({
          status: 200,
          contentType: 'text/csv',
          headers: {
            'Content-Disposition': 'attachment; filename="pvmon-report-2026-10-01_2026-10-31.csv"',
          },
          body: 'period\n',
        })
      return r.fulfill(json(reportOf(url)))
    },
  )
  const dev = devicesOf(site)
  await target.route((u) => u.pathname.startsWith('/api/v1/devices'), (r) => {
    const parts = new URL(r.request().url()).pathname.split('/').slice(4)
    if (parts.length === 0) return r.fulfill(json({ devices: dev.ids.map(dev.info) }))
    const id = parts[0] ?? ''
    if (!dev.ids.includes(id)) return r.fulfill({ status: 404, ...json({ code: 'not_found' }) })
    return r.fulfill(json(parts[1] === 'status' ? { events: dev.events(id) } : dev.detail(id)))
  })
  await target.route(
    (u) => u.pathname === '/api/v1/status',
    (r) => r.fulfill(json({ devices: dev.ids.map((id) => ({ id, changes: dev.overview(id) })) })),
  )
  const values = liveOf(site)
  await page.routeWebSocket(/\/ws$/, (ws) => {
    ws.onMessage((msg) => {
      const frame = JSON.parse(String(msg)) as { type: string; keys?: string[] }
      if (frame.type === 'ping') ws.send(JSON.stringify({ type: 'pong' }))
      if (frame.type !== 'subscribe') return
      const snapshot = Object.fromEntries(
        (frame.keys ?? [])
          .filter((k) => k in values)
          .map((k) => [k, { value: values[k], ts: now.toISOString() }]),
      )
      ws.send(JSON.stringify({ type: 'snapshot', values: snapshot }))
    })
  })
}
