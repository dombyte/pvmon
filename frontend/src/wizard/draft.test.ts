import { describe, expect, it } from 'vitest'
import type { ConfigObject, ProbeResult } from '@/api/types'
import {
  balance,
  changesOf,
  draftOf,
  emptyDeviceForm,
  formOf,
  freeID,
  objectsOf,
  proposeMeters,
  removeDevice,
  roleOf,
  upsert,
  type BlockInfo,
  type DraftObject,
} from './draft'

const obj = (
  kind: ConfigObject['kind'],
  id: string,
  body: Record<string, unknown>,
  locked = false,
): ConfigObject => ({ kind, id, body, locked, updated_at: null })

const solis: BlockInfo[] = [
  {
    name: 'backup',
    role: 'backup',
    roles_allowed: [],
    channels: ['power', 'energy_in'],
  },
  {
    name: 'battery',
    role: 'battery',
    roles_allowed: [],
    channels: ['power', 'soc'],
  },
  {
    name: 'grid',
    role: 'grid',
    roles_allowed: ['grid', 'inverter'],
    channels: ['power'],
  },
  {
    name: 'pv',
    role: 'pv',
    roles_allowed: [],
    channels: ['power', 'energy_out'],
  },
  {
    name: 'pv1',
    role: 'pv',
    roles_allowed: [],
    channels: ['current', 'power', 'voltage'],
  },
  {
    name: 'pv2',
    role: 'pv',
    roles_allowed: [],
    channels: ['current', 'power', 'voltage'],
  },
]
const shelly: BlockInfo[] = [
  { name: 'a', role: 'phase', roles_allowed: [], channels: ['power'] },
  {
    name: 'total',
    role: 'grid',
    roles_allowed: ['grid', 'consumer'],
    channels: ['power'],
  },
]
const goe: BlockInfo[] = [
  {
    name: 'charger',
    role: 'consumer',
    roles_allowed: [],
    category: 'wallbox',
    channels: ['power'],
  },
]
const blocksOf = (d: string) => (d === 'wr1' ? solis : d === 'meter' ? shelly : goe)

const probe = (powers: Record<string, number | null>): ProbeResult => ({
  device: 'wr1',
  profile: 'solis/s6-hybrid',
  verified: true,
  at: '',
  took_ms: 1,
  blocks: Object.entries(powers).map(([name, value]) => ({
    name,
    role: 'pv',
    roles_allowed: [],
    channels: [{ name: 'power', value, unit: 'W' }],
  })),
})

describe('changesOf', () => {
  it('writes new and changed UI objects and deletes removed ones', () => {
    const orig = [
      obj('device', 'wr1', { profile: 'solis/s6-hybrid', unit: 1 }),
      obj('device', 'old', { profile: 'x' }),
      obj('meter', 'grid', { device: 'meter', source: 'total' }, true),
    ]
    const draft = draftOf(orig)
    draft[0]!.body.unit = 2
    draft.splice(1, 1)
    draft.push({
      kind: 'meter',
      id: 'pv',
      body: { device: 'wr1', source: 'pv' },
      locked: false,
    })
    expect(changesOf(orig, draft)).toEqual([
      {
        kind: 'device',
        id: 'wr1',
        body: { profile: 'solis/s6-hybrid', unit: 2 },
      },
      { kind: 'meter', id: 'pv', body: { device: 'wr1', source: 'pv' } },
      { kind: 'device', id: 'old', body: null },
    ])
    expect(orig[0]!.body.unit, 'the original is untouched').toBe(1)
  })

  it('ignores key order and locked objects', () => {
    const orig = [obj('device', 'a', { unit: 1, profile: 'p' }), obj('device', 'f', {}, true)]
    const draft = draftOf(orig)
    draft[0]!.body = { profile: 'p', unit: 1 }
    draft[1]!.body.unit = 3
    expect(changesOf(orig, draft)).toEqual([])
  })
})

describe('freeID', () => {
  it('sanitises and numbers taken IDs', () => {
    const d: DraftObject[] = [{ kind: 'meter', id: 'pv', body: {}, locked: false }]
    expect(freeID('pv', 'meter', d)).toBe('pv-2')
    expect(freeID('pv', 'device', d)).toBe('pv')
    expect(freeID('S6 Hybrid_1', 'device', [])).toBe('s6-hybrid-1')
    expect(freeID('123', 'device', [])).toBe('device')
    expect(freeID('pv', 'meter', d, ['pv-2'])).toBe('pv-3')
  })
})

describe('device forms', () => {
  it('keeps TCP and HTTP addresses inline', () => {
    const f = {
      ...emptyDeviceForm(),
      id: 'wr1',
      host: '10.0.0.20',
      profile: 'solis/s6-hybrid',
    }
    expect(objectsOf(f, [])).toEqual([
      {
        kind: 'device',
        id: 'wr1',
        body: {
          profile: 'solis/s6-hybrid',
          address: 'tcp://10.0.0.20:502',
          unit: 1,
        },
        locked: false,
      },
    ])
    const h = {
      ...emptyDeviceForm(),
      id: 'warp',
      transport: 'http' as const,
      url: 'http://10.0.0.41',
      user: 'admin',
      password: 'pw',
      profile: 'tinkerforge/warp3',
      interval: '10s',
    }
    expect(objectsOf(h, [])[0]!.body).toEqual({
      profile: 'tinkerforge/warp3',
      address: 'http://10.0.0.41',
      interval: '10s',
      auth: { user: 'admin', password: 'pw' },
    })
  })

  it('creates or reuses a serial connection', () => {
    const f = {
      ...emptyDeviceForm(),
      id: 'sdm',
      transport: 'rtu' as const,
      unit: '2',
      profile: 'eastron/sdm630',
    }
    const objs = objectsOf(f, [])
    expect(objs[0]).toEqual({
      kind: 'connection',
      id: 'rtu-ttyusb0',
      body: {
        address: 'rtu:///dev/ttyUSB0',
        speed: 9600,
        parity: 'N',
        stop_bits: 1,
      },
      locked: false,
    })
    expect(objs[1]!.body).toEqual({
      profile: 'eastron/sdm630',
      connection: 'rtu-ttyusb0',
      unit: 2,
    })
    const draft = upsert([], objs)
    const back = formOf(draft[1]!, draft)
    expect(back).toMatchObject({
      transport: 'rtu',
      serial: '/dev/ttyUSB0',
      unit: '2',
    })

    const locked = draft.map((o) => ({
      ...o,
      locked: o.kind === 'connection',
    }))
    const again = objectsOf({ ...f, id: 'sdm2', speed: '19200' }, locked)
    expect(again).toHaveLength(1)
    expect(again[0]!.body.connection, 'a locked connection is used as it is').toBe('rtu-ttyusb0')
  })

  it('reads a device behind a TCP connection', () => {
    const draft = draftOf([
      obj('connection', 'lan', { address: 'tcp://10.0.0.20:1502' }, true),
      obj('device', 'wr1', { connection: 'lan', unit: 3, profile: 'solis/s6-hybrid' }, true),
    ])
    expect(formOf(draft[1]!, draft)).toMatchObject({
      transport: 'tcp',
      host: '10.0.0.20',
      port: '1502',
      unit: '3',
    })
    const f = formOf(draft[1]!, draft)
    expect(objectsOf(f, draft)[0]!.body.connection).toBe('lan')
  })

  it('reads an HTTP device', () => {
    const d = draftOf([
      obj('device', 'w', {
        address: 'http://x',
        profile: 'p',
        auth: { user: 'u', password: '***' },
      }),
    ])
    expect(formOf(d[0]!, d)).toMatchObject({
      transport: 'http',
      url: 'http://x',
      password: '***',
    })
  })

  it('removes a device with its meters and its own serial connection', () => {
    const draft = draftOf([
      obj('connection', 'rtu', { address: 'rtu:///dev/x' }),
      obj('device', 'a', { connection: 'rtu' }),
      obj('meter', 'm', { device: 'a', source: 'total' }),
      obj('meter', 'n', { device: 'b', source: 'total' }),
    ])
    expect(removeDevice(draft, 'a').map((o) => o.id)).toEqual(['n'])
    const shared = upsert(draft, [
      { kind: 'device', id: 'c', body: { connection: 'rtu' }, locked: false },
    ])
    expect(removeDevice(shared, 'a').map((o) => o.id)).toEqual(['rtu', 'n', 'c'])
  })
})

describe('proposeMeters', () => {
  it('proposes a hybrid inverter with strings below the PV total', () => {
    const { proposals, demote } = proposeMeters('wr1', solis, {
      draft: [],
      probe: probe({ pv1: 800, pv2: 0 }),
      blocksOf,
    })
    expect(demote).toEqual([])
    expect(proposals.map((p) => [p.meter.id, p.meter.body.role, p.include])).toEqual([
      ['backup', 'backup', true],
      ['battery', 'battery', true],
      ['grid', 'grid', true],
      ['pv', 'pv', true],
      ['pv-pv1', 'pv', true],
      ['pv-pv2', 'pv', false],
    ])
    expect(proposals[4]!.meter.body).toEqual({
      device: 'wr1',
      source: 'pv1',
      role: 'pv',
      parent: 'pv',
    })
  })

  it('turns the inverter grid into a diagnostic meter for a dedicated meter', () => {
    const draft = draftOf([obj('meter', 'grid', { device: 'wr1', source: 'grid', role: 'grid' })])
    const { proposals, demote } = proposeMeters('meter', shelly, {
      draft,
      blocksOf,
      saved: () => true,
    })
    expect(proposals.map((p) => [p.meter.id, p.meter.body.role])).toEqual([['grid-2', 'grid']])
    expect(demote[0]).toMatchObject({
      from: 'grid',
      meter: { id: 'grid', body: { role: 'inverter' } },
    })

    const fresh = proposeMeters('meter', shelly, { draft, blocksOf })
    expect(fresh.demote[0]!.meter.id, 'an unsaved one gives up its ID').toBe('wr1-grid')
    expect(fresh.proposals[0]!.meter.id).toBe('grid')
  })

  it('reads the role of a meter without one from its profile block', () => {
    const draft = draftOf([obj('meter', 'grid', { device: 'meter', source: 'total' }, true)])
    expect(roleOf(draft[0]!, blocksOf)).toBe('grid')
    const { proposals } = proposeMeters('wr1', solis.slice(2, 3), { draft, blocksOf })
    expect(proposals[0]!.meter.body.role).toBe('inverter')
  })

  it('makes an inverter grid diagnostic when a meter already is the grid', () => {
    const draft = draftOf([
      obj('meter', 'grid', { device: 'meter', source: 'total', role: 'grid' }),
    ])
    const { proposals } = proposeMeters('wr1', solis.slice(2, 3), { draft, blocksOf })
    expect(proposals[0]!.meter).toMatchObject({
      id: 'wr1-grid',
      body: { role: 'inverter' },
    })
    const second = proposeMeters('meter2', shelly, { draft, blocksOf }).proposals
    expect(second[0]!.meter.body).toMatchObject({
      role: 'consumer',
      category: 'other',
      parent: 'site',
    })
  })

  it('proposes consumers with their category and skips used blocks', () => {
    const draft = draftOf([obj('meter', 'pv', { device: 'wr1', source: 'pv', role: 'pv' })])
    expect(proposeMeters('goe', goe, { draft, blocksOf }).proposals[0]!.meter).toEqual({
      kind: 'meter',
      id: 'goe',
      body: {
        device: 'goe',
        source: 'charger',
        role: 'consumer',
        category: 'wallbox',
        parent: 'site',
      },
      locked: false,
    })
    const ids = proposeMeters('wr1', solis, { draft, blocksOf }).proposals.map((p) => p.meter.id)
    expect(ids).not.toContain('pv')
    expect(ids).toContain('wr1-pv1')
  })
})

describe('balance', () => {
  const meters = draftOf([
    obj('meter', 'pv', { role: 'pv' }),
    obj('meter', 'pv1', { role: 'pv', parent: 'pv' }),
    obj('meter', 'grid', { role: 'grid' }),
    obj('meter', 'battery', { role: 'battery' }),
    obj('meter', 'wb', { role: 'consumer', parent: 'site' }),
    obj('meter', 'sub', { role: 'consumer', parent: 'wb' }),
  ])

  it('adds PV, grid and battery and checks the loads', () => {
    const p: Record<string, number> = {
      pv: 3000,
      pv1: 9999,
      grid: -1000,
      battery: -500,
      wb: 1200,
      sub: 999,
    }
    const b = balance(
      meters,
      (m) => p[m.id] ?? null,
      (m) => roleOf(m, blocksOf),
    )
    expect(b).toMatchObject({
      pv: 3000,
      consumption: 1500,
      loads: 1200,
      problems: [],
      missing: [],
    })
  })

  it('reports a negative consumption, loads above it and missing values', () => {
    const p: Record<string, number | null> = {
      pv: 3000,
      grid: 1000,
      battery: null,
      wb: 200,
    }
    p.grid = -4000
    const b = balance(
      meters,
      (m) => p[m.id] ?? null,
      (m) => roleOf(m, blocksOf),
    )
    expect(b.problems).toEqual(['negative', 'loads'])
    expect(b.missing).toEqual(['battery'])
  })
})
