import type {
  AuditEntry,
  ConfigChange,
  ConfigOverview,
  EnergyLevel,
  EnergyResponse,
  DeviceDetail,
  DeviceInfo,
  DeviceStatus,
  ErrorBody,
  GaugeResponse,
  Me,
  NewToken,
  ProbeResult,
  ProfileInfo,
  ReportResponse,
  SettingView,
  SiteModel,
  StatusEvent,
  TokenInfo,
  UserInfo,
  VersionInfo,
} from './types'
import { noteResponse } from '@/pwa/offline'

/** An API error; `code` is the stable machine code that the UI translates (spec 9.2). */
class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: ErrorBody['details']

  constructor(status: number, code: string, message: string, details?: ErrorBody['details']) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }
}

/** Codes the client has a translation for (errors.<code>); others fall back to "unknown". */
const knownCodes = new Set([
  'invalid_param',
  'invalid_range',
  'range_too_long',
  'unknown_series',
  'not_found',
  'unauthorized',
  'forbidden',
  'csrf',
  'rate_limited',
  'locked_by_file',
  'validation_failed',
  'conflict',
  'internal',
  'network',
])

/** i18n key for any thrown value. */
export function errorKey(err: unknown): string {
  if (err instanceof ApiError && knownCodes.has(err.code)) return `errors.${err.code}`
  return 'errors.unknown'
}

/** Field problems of a validation_failed answer, else none. */
export function errorDetails(err: unknown): { path: string; message: string }[] {
  return err instanceof ApiError ? (err.details ?? []) : []
}

function isErrorBody(v: unknown): v is ErrorBody {
  return typeof v === 'object' && v !== null && typeof (v as ErrorBody).code === 'string'
}

async function request(
  path: string,
  signal?: AbortSignal,
  init: RequestInit = {},
): Promise<Response> {
  let res: Response
  try {
    res = await fetch(path, {
      ...init,
      headers: { Accept: 'application/json', ...init.headers },
      signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError(0, 'network', 'network error')
  }
  noteResponse(res)
  if (!res.ok) {
    let body: unknown
    try {
      body = await res.json()
    } catch {
      body = null
    }
    if (isErrorBody(body)) {
      throw new ApiError(res.status, body.code, body.message, body.details)
    }
    throw new ApiError(res.status, res.status >= 500 ? 'internal' : 'unknown', res.statusText)
  }
  return res
}

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  return (await (await request(path, signal)).json()) as T
}

export const fetchSite = (signal?: AbortSignal) => getJSON<SiteModel>('/api/v1/site', signal)

export const fetchVersion = (signal?: AbortSignal) =>
  getJSON<VersionInfo>('/api/v1/version', signal)

export interface EnergyQuery {
  series: string[]
  level: EnergyLevel
  from: string
  to: string
}

export function fetchEnergy(q: EnergyQuery, signal?: AbortSignal): Promise<EnergyResponse> {
  const params = new URLSearchParams({
    series: q.series.join(','),
    level: q.level,
    from: q.from,
    to: q.to,
  })
  return getJSON<EnergyResponse>(`/api/v1/energy?${params.toString()}`, signal)
}

export interface GaugeQuery {
  series: string[]
  from: string
  to: string
}

export function fetchGauge(q: GaugeQuery, signal?: AbortSignal): Promise<GaugeResponse> {
  const params = new URLSearchParams({
    series: q.series.join(','),
    from: q.from,
    to: q.to,
  })
  return getJSON<GaugeResponse>(`/api/v1/gauge?${params.toString()}`, signal)
}

export const fetchDevices = (_q: true, signal?: AbortSignal) =>
  getJSON<{ devices: DeviceInfo[] }>('/api/v1/devices', signal)

export const fetchDevice = (id: string, signal?: AbortSignal) =>
  getJSON<DeviceDetail>(`/api/v1/devices/${encodeURIComponent(id)}`, signal)

export interface StatusQuery {
  id: string
  start: string
  end: string
}

export function fetchStatus(q: StatusQuery, signal?: AbortSignal) {
  const params = new URLSearchParams({ start: q.start, end: q.end })
  return getJSON<{ events: StatusEvent[] }>(
    `/api/v1/devices/${encodeURIComponent(q.id)}/status?${params.toString()}`,
    signal,
  )
}

export function fetchStatusOverview(q: { start: string; end: string }, signal?: AbortSignal) {
  const params = new URLSearchParams(q)
  return getJSON<{ devices: DeviceStatus[] }>(`/api/v1/status?${params.toString()}`, signal)
}

export interface ReportQuery {
  from: string
  to: string
  compare: 'last_year' | 'previous' | 'none'
}

function reportParams(q: ReportQuery, extra: Record<string, string> = {}): string {
  return new URLSearchParams({
    from: q.from,
    to: q.to,
    compare: q.compare,
    ...extra,
  }).toString()
}

export const fetchReport = (q: ReportQuery, signal?: AbortSignal) =>
  getJSON<ReportResponse>(`/api/v1/report?${reportParams(q)}`, signal)

/** A report file with the name the server gave it. */
export interface ReportFile {
  name: string
  blob: Blob
}

export async function downloadReport(
  q: ReportQuery,
  format: 'pdf' | 'csv',
  lang: string,
): Promise<ReportFile> {
  const res = await request(`/api/v1/report?${reportParams(q, { format, lang })}`)
  const name = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '')?.[1]
  return { name: name ?? `pvmon-report.${format}`, blob: await res.blob() }
}

/** Sends a JSON body; the answer is parsed unless it is 204. Same-origin fetches carry
 * Origin, which the CSRF check needs (spec 10). */
async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method }
  if (body !== undefined) {
    init.body = JSON.stringify(body)
    init.headers = { 'Content-Type': 'application/json' }
  }
  const res = await request(path, undefined, init)
  return (res.status === 204 ? undefined : await res.json()) as T
}

export const fetchMe = (signal?: AbortSignal) => getJSON<Me>('/api/v1/auth/me', signal)

export const login = (name: string, password: string) =>
  send<{ user: UserInfo }>('POST', '/api/v1/auth/login', { name, password })

export const logout = () => send<void>('POST', '/api/v1/auth/logout')

export interface SetupRequest {
  code: string
  name: string
  password: string
  lang: string
}

export const setup = (r: SetupRequest) => send<{ user: UserInfo }>('POST', '/api/v1/setup', r)

export const fetchUsers = (_q: true, signal?: AbortSignal) =>
  getJSON<{ users: UserInfo[] }>('/api/v1/users', signal)

export const addUser = (name: string, password: string, lang: string) =>
  send<{ user: UserInfo }>('POST', '/api/v1/users', { name, password, lang })

export const deleteUser = (id: number) => send<void>('DELETE', `/api/v1/users/${id}`)

export const setPassword = (id: number, password: string) =>
  send<void>('PUT', `/api/v1/users/${id}/password`, { password })

export const fetchTokens = (_q: true, signal?: AbortSignal) =>
  getJSON<{ tokens: TokenInfo[] }>('/api/v1/tokens', signal)

export const createToken = (name: string, expiresDays: number) =>
  send<NewToken>('POST', '/api/v1/tokens', { name, expires_days: expiresDays })

export const revokeToken = (id: string) =>
  send<void>('DELETE', `/api/v1/tokens/${encodeURIComponent(id)}`)

/** The last 30 days (the server's default range). */
export const fetchAudit = (_q: unknown, signal?: AbortSignal) =>
  getJSON<{ entries: AuditEntry[] }>('/api/v1/audit', signal)

export const fetchConfig = (_q: true, signal?: AbortSignal) =>
  getJSON<ConfigOverview>('/api/v1/config', signal)

export const putSettings = (set: Record<string, unknown>) =>
  send<{ config_rev: number; settings: Record<string, SettingView> }>(
    'PUT',
    '/api/v1/config/settings',
    set,
  )

/** Several object changes as one revision (spec 9.1). */
export const putObjects = (objects: ConfigChange[]) =>
  send<ConfigOverview>('PUT', '/api/v1/config', { objects })

/** Reads a device once with the draft changes applied; nothing is stored. */
export const probeDevice = (objects: ConfigChange[], device: string) =>
  send<ProbeResult>('POST', '/api/v1/config/probe', { objects, device })

export const fetchProfiles = (_q: true, signal?: AbortSignal) =>
  getJSON<{ profiles: ProfileInfo[] }>('/api/v1/profiles', signal)

/** The configuration export as YAML (secrets masked). */
export async function downloadConfig(): Promise<Blob> {
  return (await request('/api/v1/config/export')).blob()
}
