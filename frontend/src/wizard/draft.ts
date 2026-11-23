// The setup wizard's draft (spec 11.14): the UI configuration as editable objects, the
// meter proposal from a profile, the changes to save and the plausibility balance. Pure:
// no API calls, no Vue.
import type { ConfigChange, ConfigObject, ProbeBlock, ProbeResult } from '@/api/types'

export type Kind = ConfigObject['kind']
export type Body = Record<string, unknown>

/** One object of the draft; locked objects come from config.yaml or env. */
export interface DraftObject {
  kind: Kind
  id: string
  body: Body
  locked: boolean
}

/** A block of a device's profile, from the profile list or a probe. */
export type BlockInfo = Pick<ProbeBlock, 'name' | 'role' | 'roles_allowed' | 'category'> & {
  /** Channel names; a PV string has a power product (voltage × current). */
  channels: string[]
}

const idPattern = /^[a-z][a-z0-9-]{0,31}$/
const maxIDLength = 32

/** The draft of an overview; bodies are copied, so editing never touches the original. */
export function draftOf(objects: ConfigObject[]): DraftObject[] {
  return objects.map((o) => ({
    kind: o.kind,
    id: o.id,
    body: clone(o.body),
    locked: o.locked,
  }))
}

function clone(b: Body): Body {
  return JSON.parse(JSON.stringify(b)) as Body
}

/** Stable JSON: object keys sorted, so equal bodies compare equal. */
function stable(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(stable).join(',')}]`
  if (v !== null && typeof v === 'object') {
    const o = v as Record<string, unknown>
    const keys = Object.keys(o)
      .filter((k) => o[k] !== undefined)
      .sort()
    return `{${keys.map((k) => `${JSON.stringify(k)}:${stable(o[k])}`).join(',')}}`
  }
  return JSON.stringify(v)
}

const key = (o: { kind: Kind; id: string }) => `${o.kind}/${o.id}`

/** What saving the draft writes: new and changed UI objects, deletions as null bodies. */
export function changesOf(original: ConfigObject[], draft: DraftObject[]): ConfigChange[] {
  const before = new Map(original.map((o) => [key(o), o]))
  const after = new Set(draft.map(key))
  const out: ConfigChange[] = []
  for (const o of draft) {
    if (o.locked) continue
    const prev = before.get(key(o))
    if (!prev || stable(prev.body) !== stable(o.body)) {
      out.push({ kind: o.kind, id: o.id, body: o.body })
    }
  }
  for (const o of original) {
    if (!o.locked && !after.has(key(o))) out.push({ kind: o.kind, id: o.id, body: null })
  }
  return out
}

/** A free ID for an object of a kind: the wish if valid and free, else with a number. */
export function freeID(wish: string, kind: Kind, draft: DraftObject[], taken: string[] = []) {
  let base = wish
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^[^a-z]+/, '')
    .replace(/-+$/, '')
    .slice(0, maxIDLength - 3)
  if (!idPattern.test(base)) base = kind
  const used = new Set([...draft.filter((o) => o.kind === kind).map((o) => o.id), ...taken])
  if (!used.has(base)) return base
  for (let n = 2; ; n++) {
    const id = `${base}-${n}`
    if (!used.has(id)) return id
  }
}

export const isValidID = (id: string) => idPattern.test(id)

// --- devices ---------------------------------------------------------------------------

export type Transport = 'tcp' | 'rtu' | 'http'

/** The device form of the wizard; strings, as the inputs edit them. */
export interface DeviceForm {
  id: string
  transport: Transport
  host: string
  port: string
  serial: string
  speed: string
  parity: string
  stopBits: string
  url: string
  user: string
  password: string
  unit: string
  profile: string
  interval: string
}

const defaultTCPPort = '502'
const defaultSpeed = '9600'

export function emptyDeviceForm(): DeviceForm {
  return {
    id: '',
    transport: 'tcp',
    host: '',
    port: defaultTCPPort,
    serial: '/dev/ttyUSB0',
    speed: defaultSpeed,
    parity: 'N',
    stopBits: '1',
    url: 'http://',
    user: '',
    password: '',
    unit: '1',
    profile: '',
    interval: '',
  }
}

const str = (v: unknown) => (typeof v === 'string' || typeof v === 'number' ? String(v) : '')

/** The address of a device: its own or that of its connection. */
export function deviceAddress(dev: DraftObject, draft: DraftObject[]): string {
  const conn = draft.find((o) => o.kind === 'connection' && o.id === dev.body.connection)
  return str(conn?.body.address ?? dev.body.address)
}

/** The form of an existing device. */
export function formOf(dev: DraftObject, draft: DraftObject[]): DeviceForm {
  const f = emptyDeviceForm()
  const b = dev.body
  const conn = draft.find((o) => o.kind === 'connection' && o.id === b.connection)
  const addr = deviceAddress(dev, draft)
  const auth = (b.auth ?? {}) as Body
  Object.assign(f, {
    id: dev.id,
    unit: str(b.unit) || f.unit,
    profile: str(b.profile),
    interval: str(b.interval),
    user: str(auth.user),
    password: str(auth.password),
  })
  const m = /^(tcp|rtu|https?):\/\/(.*)$/.exec(addr)
  if (m?.[1] === 'tcp') {
    const [host, port] = m[2]!.split(':')
    Object.assign(f, {
      transport: 'tcp',
      host: host ?? '',
      port: port ?? defaultTCPPort,
    })
  } else if (m?.[1] === 'rtu') {
    const c = conn?.body ?? {}
    Object.assign(f, {
      transport: 'rtu',
      serial: m[2],
      speed: str(c.speed) || f.speed,
      parity: str(c.parity) || f.parity,
      stopBits: str(c.stop_bits) || f.stopBits,
    })
  } else if (m) {
    Object.assign(f, { transport: 'http', url: addr })
  }
  return f
}

/** The address a form describes. */
function formAddress(f: DeviceForm): string {
  if (f.transport === 'tcp') return `tcp://${f.host.trim()}:${f.port.trim() || defaultTCPPort}`
  if (f.transport === 'rtu') return `rtu://${f.serial.trim()}`
  return f.url.trim()
}

const num = (s: string) => (s.trim() !== '' && Number.isFinite(Number(s)) ? Number(s) : s)

/**
 * The objects a form writes: the device, plus a connection for a serial port nobody uses
 * yet (TCP and HTTP devices carry their address inline; the server merges equal addresses).
 * A device keeps an existing connection with the same address.
 */
export function objectsOf(f: DeviceForm, draft: DraftObject[]): DraftObject[] {
  const addr = formAddress(f)
  const body: Body = { profile: f.profile.trim() }
  const out: DraftObject[] = []
  let conn = draft.find((o) => o.kind === 'connection' && o.body.address === addr)
  if (f.transport === 'rtu') {
    const serial = {
      speed: num(f.speed),
      parity: f.parity,
      stop_bits: num(f.stopBits),
    }
    if (!conn) {
      const id = freeID(`rtu-${f.serial.split('/').pop() ?? ''}`, 'connection', draft)
      conn = {
        kind: 'connection',
        id,
        body: { address: addr, ...serial },
        locked: false,
      }
      out.push(conn)
    } else if (!conn.locked) {
      conn = { ...conn, body: { ...conn.body, ...serial } }
      out.push(conn)
    }
  }
  if (conn) body.connection = conn.id
  else body.address = addr
  if (f.transport !== 'http') body.unit = num(f.unit)
  if (f.interval.trim()) body.interval = f.interval.trim()
  if (f.transport === 'http' && f.user.trim()) {
    body.auth = { user: f.user.trim(), password: f.password }
  }
  out.push({ kind: 'device', id: f.id, body, locked: false })
  return out
}

/** Replaces or adds objects by kind and ID. */
export function upsert(draft: DraftObject[], objs: DraftObject[]): DraftObject[] {
  const out = [...draft]
  for (const o of objs) {
    const i = out.findIndex((x) => key(x) === key(o))
    if (i >= 0) out[i] = o
    else out.push(o)
  }
  return out
}

/** Removes a device with its meters and the serial connection only it used. */
export function removeDevice(draft: DraftObject[], id: string): DraftObject[] {
  const dev = draft.find((o) => o.kind === 'device' && o.id === id)
  const conn = dev?.body.connection
  const others = draft.some((o) => o.kind === 'device' && o.id !== id && o.body.connection === conn)
  return draft.filter(
    (o) =>
      !(o.kind === 'device' && o.id === id) &&
      !(o.kind === 'meter' && o.body.device === id && !o.locked) &&
      !(o.kind === 'connection' && o.id === conn && !others && !o.locked),
  )
}

/** The changes a probe needs: the device and its connection as the form describes them. */
export function probeChanges(objs: DraftObject[]): ConfigChange[] {
  return objs.filter((o) => !o.locked).map((o) => ({ kind: o.kind, id: o.id, body: o.body }))
}

// --- meters ----------------------------------------------------------------------------

/** What the proposal needs besides the device: the draft, its probe and other profiles. */
export interface ProposalContext {
  draft: DraftObject[]
  probe?: ProbeResult
  /** The blocks of another device, to see whether the grid meter is an inverter's. */
  blocksOf: (device: string) => BlockInfo[]
  /** Whether a meter is stored already; only unsaved ones are renamed. */
  saved?: (id: string) => boolean
}

/** A meter that turns diagnostic, possibly under a new ID. */
export interface Demotion {
  from: string
  meter: DraftObject
}

/** The role of a meter: its own, else the profile's role for its block (spec 3). */
export function roleOf(m: DraftObject, blocksOf: (device: string) => BlockInfo[]): string {
  if (typeof m.body.role === 'string' && m.body.role) return m.body.role
  return blocksOf(String(m.body.device)).find((b) => b.name === m.body.source)?.role ?? ''
}

/** A proposed meter; include is the suggestion whether to add it. */
export interface Proposal {
  meter: DraftObject
  include: boolean
}

const topRoles = ['grid', 'battery', 'pv', 'backup']

/** The power of a block in a probe, null without a value. */
export function blockPower(probe: ProbeResult | undefined, block: string): number | null {
  const b = probe?.blocks.find((x) => x.name === block)
  return b?.channels.find((c) => c.name === 'power')?.value ?? null
}

/** A PV string: its power is computed from voltage and current. */
const isString = (b: BlockInfo) =>
  b.role === 'pv' && b.channels.includes('voltage') && b.channels.includes('current')

/** Whether a grid block belongs to an inverter (it may become a diagnostic meter). */
const inverterGrid = (b: BlockInfo | undefined) => b?.roles_allowed.includes('inverter') ?? false

/**
 * The meters proposed for a device (spec 11.14 step 3): one per block of the profile,
 * phases left out. A second grid block becomes `inverter` (diagnostic) when its profile
 * allows it, a `consumer` otherwise. PV strings sit below the device's PV total and are
 * suggested only when the probe shows them delivering.
 */
export function proposeMeters(
  device: string,
  blocks: BlockInfo[],
  ctx: ProposalContext,
): { proposals: Proposal[]; demote: Demotion[] } {
  const { draft, probe } = ctx
  const meters = draft.filter((o) => o.kind === 'meter')
  const used = new Set(meters.filter((m) => m.body.device === device).map((m) => m.body.source))
  const taken: string[] = []
  const proposals: Proposal[] = []
  const demote: Demotion[] = []
  // IDs given up by a renamed demotion are free again.
  const others = () =>
    draft.filter((o) => !demote.some((d) => d.from === o.id && o.kind === 'meter'))
  const add = (wish: string, body: Body, include = true) => {
    const id = freeID(wish, 'meter', others(), taken)
    taken.push(id)
    const meter: DraftObject = {
      kind: 'meter',
      id,
      body: { device, ...body },
      locked: false,
    }
    proposals.push({ meter, include })
    return meter
  }
  let pvTotal: DraftObject | undefined
  for (const b of blocks.filter((x) => !isString(x))) {
    if (used.has(b.name) || b.role === 'phase' || b.role === 'inverter') continue
    if (b.role === 'grid') {
      const r = gridRole(b, meters, ctx)
      if (r.demote) {
        demote.push(r.demote)
        taken.push(r.demote.meter.id)
      }
      if (r.role === 'consumer') {
        add(device, {
          source: b.name,
          role: 'consumer',
          category: 'other',
          parent: 'site',
        })
      } else if (r.role)
        add(r.role === 'grid' ? 'grid' : `${device}-grid`, {
          source: b.name,
          role: r.role,
        })
      continue
    }
    if (b.role === 'consumer') {
      add(device, {
        source: b.name,
        role: 'consumer',
        category: b.category ?? 'other',
        parent: 'site',
      })
      continue
    }
    const wish = topRoles.includes(b.role) && b.name === b.role ? b.role : `${device}-${b.name}`
    const m = add(wish, { source: b.name, role: b.role })
    if (b.role === 'pv' && !pvTotal) pvTotal = m
  }
  for (const b of blocks.filter(isString)) {
    if (used.has(b.name)) continue
    const power = blockPower(probe, b.name)
    const body: Body = { source: b.name, role: 'pv' }
    if (pvTotal) body.parent = pvTotal.id
    add(`${pvTotal?.id ?? device}-${b.name}`, body, power !== null && power > 0)
  }
  return { proposals, demote }
}

/** The role of a new grid block: grid, inverter (diagnostic), consumer, or none. */
function gridRole(
  b: BlockInfo,
  meters: DraftObject[],
  ctx: ProposalContext,
): { role: string | null; demote?: Demotion } {
  const grid = meters.find((m) => roleOf(m, ctx.blocksOf) === 'grid')
  if (!grid) return { role: 'grid' }
  const gridBlock = ctx.blocksOf(String(grid.body.device)).find((x) => x.name === grid.body.source)
  // A dedicated meter replaces the inverter's grid channel, which becomes diagnostic; an
  // unsaved one also gives up the ID "grid".
  if (inverterGrid(gridBlock) && !inverterGrid(b) && !grid.locked) {
    const rest = meters.filter((m) => m.id !== grid.id)
    const id = ctx.saved?.(grid.id)
      ? grid.id
      : freeID(`${String(grid.body.device)}-grid`, 'meter', rest)
    return {
      role: 'grid',
      demote: { from: grid.id, meter: { ...grid, id, body: { ...grid.body, role: 'inverter' } } },
    }
  }
  if (b.roles_allowed.includes('inverter')) return { role: 'inverter' }
  if (b.roles_allowed.includes('consumer')) return { role: 'consumer' }
  return { role: null }
}

// --- plausibility ----------------------------------------------------------------------

/** Tolerance of the balance check: 100 W or 5 % of the larger flow. */
const toleranceW = 100
const tolerancePct = 0.05

interface Balance {
  pv: number
  grid: number
  battery: number
  consumption: number
  loads: number
  /** Meters without a value, so the balance is incomplete. */
  missing: string[]
  problems: ('negative' | 'loads')[]
}

/**
 * The live balance of the draft (spec 11.14 step 5, signs as in spec 2): consumption =
 * PV + grid + battery; it must not be negative, and the consumers directly under the site
 * must fit into it, both within a tolerance.
 */
export function balance(
  draft: DraftObject[],
  power: (meter: DraftObject) => number | null,
  roleOf: (meter: DraftObject) => string,
) {
  const out: Balance = {
    pv: 0,
    grid: 0,
    battery: 0,
    consumption: 0,
    loads: 0,
    missing: [],
    problems: [],
  }
  for (const m of draft.filter((o) => o.kind === 'meter')) {
    const role = roleOf(m)
    const top = !m.body.parent || m.body.parent === 'site'
    const counted =
      (role === 'pv' && top) ||
      role === 'grid' ||
      role === 'battery' ||
      (role === 'consumer' && top)
    if (!counted) continue
    const p = power(m)
    if (p === null) {
      out.missing.push(m.id)
      continue
    }
    if (role === 'pv') out.pv += p
    else if (role === 'grid') out.grid += p
    else if (role === 'battery') out.battery += p
    else out.loads += p
  }
  out.consumption = out.pv + out.grid + out.battery
  const scale = Math.max(Math.abs(out.pv), Math.abs(out.grid), Math.abs(out.battery), out.loads)
  const tol = Math.max(toleranceW, scale * tolerancePct)
  if (out.consumption < -tol) out.problems.push('negative')
  if (out.loads > out.consumption + tol) out.problems.push('loads')
  return out
}
