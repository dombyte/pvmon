// Answers of the admin endpoints (auth, users, tokens, audit, configuration) for the
// settings, login and setup pages.
import type { BrowserContext, Page, Route } from '@playwright/test'
import type {
  AuditEntry,
  ConfigChange,
  ConfigOverview,
  ProbeResult,
  ProfileInfo,
  TokenInfo,
  UserInfo,
} from '../src/api/types'

const users: UserInfo[] = [
  { id: 1, name: 'admin', lang: 'de', created_at: '2026-11-01T09:12:00Z', last_login: '2026-11-19T11:02:41Z' },
  { id: 2, name: 'anna', created_at: '2026-11-10T17:40:00Z', last_login: null },
]

const tokens: TokenInfo[] = [
  {
    id: 'k3f9a2',
    name: 'home-assistant',
    scope: 'admin',
    created_at: '2026-11-02T10:00:00Z',
    expires_at: '2027-11-02T10:00:00Z',
    last_used: '2026-11-19T12:40:00Z',
  },
]

const audit: AuditEntry[] = [
  {
    id: 3,
    at: '2026-11-19T12:30:00Z',
    actor: 'user:admin',
    action: 'config_change',
    detail: {
      changes: [{ op: 'settings', changed: { 'site.name': ['Home', 'Haus'] } }],
    },
  },
  { id: 2, at: '2026-11-19T11:02:41Z', actor: 'user:admin', action: 'login', detail: { ip: '192.168.1.5' } },
  { id: 1, at: '2026-11-01T09:12:00Z', actor: 'user:admin', action: 'setup', detail: { ip: '192.168.1.5' } },
]

const config: ConfigOverview = {
  config_rev: 4,
  objects: [
    { kind: 'connection', id: 'lan', locked: true, body: { address: 'tcp://192.168.1.20:502' }, updated_at: null },
    { kind: 'device', id: 'wr1', locked: true, body: { connection: 'lan', profile: 'solis/s6-hybrid', unit: 1 }, updated_at: null },
    {
      kind: 'device',
      id: 'shelly',
      locked: false,
      body: { connection: 'http-shelly', profile: 'shelly/pro3em' },
      updated_at: '2026-11-18T19:21:07Z',
      updated_by: 'user:admin',
    },
    { kind: 'meter', id: 'grid', locked: false, body: { device: 'shelly', source: 'grid' }, updated_at: '2026-11-18T19:21:07Z' },
  ],
  settings: {
    'site.name': { value: 'Haus', locked: false, stored: true },
    'site.interval': { value: '5s', locked: false, stored: false },
    'status.offline_after': { value: '15m', locked: false, stored: false },
    'auth.public_read': { value: true, locked: true, stored: false },
    'storage.retention': { value: '10y', locked: false, stored: false },
    'storage.backup_interval': { value: '24h', locked: false, stored: false },
    'storage.max_backups': { value: 7, locked: false, stored: false },
  },
}

const block = (name: string, role: string, channels: string[], roles_allowed: string[] = []) => ({
  name,
  role,
  roles_allowed,
  channels,
})

const profiles: ProfileInfo[] = [
  {
    id: 'shelly/pro3em',
    vendor: 'Shelly',
    models: ['Pro 3EM'],
    transport: 'http',
    verified: true,
    blocks: [
      block('a', 'phase', ['power', 'voltage']),
      block('grid', 'grid', ['energy_in', 'energy_out', 'power'], ['grid', 'consumer']),
    ],
  },
  {
    id: 'solis/s6-hybrid',
    vendor: 'Solis',
    models: ['S6-EH1P*'],
    transport: 'modbus',
    verified: true,
    blocks: [
      block('battery', 'battery', ['power', 'soc']),
      block('grid', 'grid', ['power'], ['grid', 'inverter']),
      block('pv', 'pv', ['energy_out', 'power']),
      block('pv1', 'pv', ['current', 'power', 'voltage']),
      block('pv2', 'pv', ['current', 'power', 'voltage']),
    ],
  },
]

/** Live values per profile and block; a device at 10.0.0.99 does not answer. */
const probeValues: Record<string, Record<string, Record<string, number>>> = {
  'shelly/pro3em': { a: { power: -510, voltage: 231.2 }, grid: { power: -1500, energy_in: 812345 } },
  'solis/s6-hybrid': {
    battery: { power: -500, soc: 64 },
    grid: { power: -1480 },
    pv: { power: 3200, energy_out: 15200 },
    pv1: { power: 3200, voltage: 412.5, current: 7.76 },
    pv2: { power: 0, voltage: 0, current: 0 },
  },
}

function probe(r: Route) {
  const req = r.request().postDataJSON() as { objects: ConfigChange[]; device: string }
  const body =
    req.objects.find((o) => o.kind === 'device' && o.id === req.device)?.body ??
    config.objects.find((o) => o.kind === 'device' && o.id === req.device)?.body ??
    {}
  const prof = profiles.find((p) => p.id === body.profile)
  if (!prof) return r.fulfill(json({ code: 'not_found', message: 'x' }, 404))
  const res: ProbeResult = {
    device: req.device,
    profile: prof.id,
    verified: true,
    at: '2026-11-19T12:00:00Z',
    took_ms: 84,
    blocks: prof.blocks.map((b) => ({
      ...b,
      channels: b.channels.map((c) => ({
        name: c,
        value: probeValues[prof.id]?.[b.name]?.[c] ?? null,
        unit: c === 'power' ? 'W' : c.startsWith('energy') ? 'Wh' : c === 'soc' ? '%' : c === 'current' ? 'A' : 'V',
      })),
    })),
  }
  if (String(body.address).includes('10.0.0.99')) {
    res.error = 'collector: wr1 read 33035+24: modbus: i/o timeout'
  }
  return r.fulfill(json(res))
}

/** PUT /api/v1/config: a grid meter is required once meters exist. */
function putObjects(r: Route) {
  const { objects } = r.request().postDataJSON() as { objects: ConfigChange[] }
  if (objects.some((o) => o.kind === 'meter' && o.id === 'grid' && o.body === null)) {
    return r.fulfill(
      json(
        {
          code: 'validation_failed',
          message: 'x',
          details: [{ path: 'meters', message: 'exactly one grid meter is required, found none' }],
        },
        422,
      ),
    )
  }
  return r.fulfill(json({ ...config, config_rev: 5 }))
}

const json = (body: unknown, status = 200) => ({
  status,
  contentType: 'application/json',
  body: JSON.stringify(body),
})

/** A password the mock rejects, to show the policy message. */
export const shortPassword = 'short'

function login(r: Route) {
  const body = r.request().postDataJSON() as { name: string; password: string }
  if (body.password !== 'correct horse battery') {
    return r.fulfill(json({ code: 'unauthorized', message: 'invalid name or password' }, 401))
  }
  return r.fulfill(json({ user: users[0] }))
}

function setup(r: Route) {
  const body = r.request().postDataJSON() as { code: string; password: string }
  if (body.code !== '123456') return r.fulfill(json({ code: 'forbidden', message: 'x' }, 403))
  if (body.password === shortPassword) {
    return r.fulfill(
      json(
        {
          code: 'validation_failed',
          message: 'x',
          details: [{ path: 'password', message: 'too_short' }],
        },
        422,
      ),
    )
  }
  return r.fulfill(json({ user: users[0] }, 201))
}

export async function mockAdmin(target: Page | BrowserContext): Promise<void> {
  await target.route('**/api/v1/auth/login', login)
  await target.route('**/api/v1/setup', setup)
  await target.route('**/api/v1/auth/logout', (r) => r.fulfill({ status: 204 }))
  await target.route('**/api/v1/users', (r) => r.fulfill(json({ users })))
  await target.route('**/api/v1/tokens', (r) =>
    r.request().method() === 'POST'
      ? r.fulfill(json({ ...tokens[0], id: 'n3w', name: 'grafana', token: 'pvm_n3w_s3cr3tvalue' }, 201))
      : r.fulfill(json({ tokens })),
  )
  await target.route('**/api/v1/audit', (r) => r.fulfill(json({ entries: audit })))
  await target.route('**/api/v1/config', (r) =>
    r.request().method() === 'PUT' ? putObjects(r) : r.fulfill(json(config)),
  )
  await target.route('**/api/v1/config/probe', probe)
  await target.route('**/api/v1/profiles', (r) => r.fulfill(json({ profiles })))
  await target.route('**/api/v1/config/settings', (r) => {
    const set = r.request().postDataJSON() as Record<string, unknown>
    if (set['site.interval'] === 'soon') {
      return r.fulfill(
        json(
          {
            code: 'validation_failed',
            message: 'x',
            details: [{ path: 'site.interval', message: 'invalid duration "soon"' }],
          },
          422,
        ),
      )
    }
    return r.fulfill(json({ config_rev: 5, settings: config.settings }))
  })
}
