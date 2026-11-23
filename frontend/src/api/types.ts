// Types of the JSON the Go server returns (internal/service/site.go, handler.go).

interface Capabilities {
  pv: number
  battery: number
  grid: boolean
  backup: boolean
  loadpoints: Record<string, number>
}

interface Meter {
  id: string
  role: string
  category?: string
  label: string
  device: string
  parent?: string
  capacity_wh?: number
  kwp?: number
  series: string[]
}

export type DeviceState = 'online' | 'offline' | 'stale' | 'unconfigured'

interface Device {
  id: string
  profile: string
  verified: boolean
  state: DeviceState
}

export interface SiteModel {
  name: string
  tz: string
  config_rev: number
  data_since: string | null
  capabilities: Capabilities
  meters: Meter[]
  devices: Device[]
  site_series: string[]
}

export interface VersionInfo {
  version: string
  commit: string
  build_date: string
  go: string
}

/** Error body (spec 9.2). */
export interface ErrorBody {
  error: string
  code: string
  message: string
  details?: { path: string; message: string }[]
}

/** One period of GET /api/v1/energy; the number is null when there is no data. */
export interface EnergyValue {
  period: string
  wh?: number | null
  pct?: number | null
  complete: boolean
  estimated: boolean
  missing_days: number
  /** Incomplete local days of the period, sorted. */
  missing: string[]
}

export interface EnergySeries {
  name: string
  unit: 'Wh' | '%'
  values: EnergyValue[]
}

export interface EnergyResponse {
  series: EnergySeries[]
}

export type EnergyLevel = 'bucket' | 'day' | 'month' | 'year' | 'total'

/** One 15-minute slot of GET /api/v1/gauge; slots without data are absent. */
export interface GaugePoint {
  at: string
  avg: number
  min: number
  max: number
  cover: number
}

export interface GaugeSeries {
  name: string
  unit: string
  points: GaugePoint[]
}

export interface GaugeResponse {
  series: GaugeSeries[]
}

/** One device of GET /api/v1/devices. */
export interface DeviceInfo {
  id: string
  profile: string
  vendor: string
  models: string[]
  transport: string
  verified: boolean
  state: DeviceState
  interval_s: number
  meters: string[]
  /** Newest poll since the server started; null before the first. */
  last_poll: { at: string; took_ms: number; ok: boolean } | null
}

interface DeviceChannel {
  name: string
  /** Live key for the WebSocket. */
  key: string
  unit?: string
}

interface DeviceBlock {
  meter: string
  label: string
  role: string
  block: string
  channels: DeviceChannel[]
}

/** GET /api/v1/devices/{id}. */
export interface DeviceDetail extends DeviceInfo {
  netting?: string
  read?: string
  blocks: DeviceBlock[]
  status: StatusRegister[]
}

/** A status register of the device profile with its optional display name. */
interface StatusRegister {
  name: string
  label?: Record<string, string>
}

/**
 * Generic level (spec 12): the profile levels, plus offline (the device does not answer)
 * and none (pvmon did not run).
 */
export type StatusLevel = 'ok' | 'notice' | 'fault' | 'offline' | 'none'

/** The status register that holds the device connection. */
export const connectionRegister = '@connection'

/** One device of GET /api/v1/status: its generic level over time, oldest first. */
export interface DeviceStatus {
  id: string
  changes: { at: string; level: StatusLevel }[]
}

/** One status change of GET /api/v1/devices/{id}/status. */
export interface StatusEvent {
  register: string
  at: string
  code: number
  level: StatusLevel
  texts: Record<string, string>[] | null
}

/** Site quantities of a report in kWh; quantities the site does not have are null. */
interface ReportEnergy {
  pv_kwh: number | null
  consumption_kwh: number
  import_kwh: number
  export_kwh: number
  charge_kwh: number | null
  discharge_kwh: number | null
}

interface ReportSplit {
  solar_est_kwh: number
  battery_est_kwh: number
  grid_est_kwh: number
  solar_pct: number | null
  battery_pct: number | null
  grid_pct: number | null
}

export interface ReportFigure {
  id: string
  kind: 'energy' | 'ratio'
  better: 'higher' | 'lower' | 'neutral'
  value: number | null
  compared: number | null
  base: number | null
  change: number | null
  change_pct: number | null
}

interface ReportMeter {
  id: string
  label: string
  category?: string
  complete_days: number
  in_kwh: number | null
  out_kwh: number | null
  share_pct?: number | null
  specific_yield?: number | null
  ratio?: number | null
  solar_est_kwh?: number | null
  solar_pct?: number | null
}

export interface ReportBucket {
  key: string
  from: string
  to: string
  partial: boolean
  days: number
  complete_days: number
  total: ReportEnergy
  self_sufficiency_pct: number | null
}

interface ReportDay {
  date: string
  kwh: number
}

/** GET /api/v1/report (spec 9.1, 12). */
export interface ReportResponse {
  from: string
  to: string
  granularity: 'day' | 'month' | 'year'
  today_excluded: boolean
  summary: {
    days: number
    complete_days: number
    total: ReportEnergy
    self_sufficiency_pct: number | null
    self_consumption_pct: number | null
    grid_charge_est_kwh: number | null
    sources: ReportSplit
    pv_split: ReportSplit | null
    battery_ratio: number | null
    balance_diff_kwh: number | null
    balance_diff_pct: number | null
    fallback_days: number
  }
  figures: ReportFigure[]
  comparison: {
    mode: string
    from: string
    to: string
    days: number
    complete_days: number
    matched: boolean
    matched_days: number
  } | null
  buckets: ReportBucket[]
  pv_sources: ReportMeter[]
  batteries: ReportMeter[]
  loadpoints: {
    meters: ReportMeter[]
    categories: ReportMeter[] | null
  } | null
  patterns: {
    best_pv: ReportDay | null
    lowest_pv: ReportDay | null
    max_consumption: ReportDay | null
    max_import: ReportDay | null
    weekday_consumption_kwh: number | null
    weekend_consumption_kwh: number | null
    low_import_days: number
  }
  missing_days: string[]
  quality: {
    id: string
    label: string
    missing_days: number
    estimated_days: number
  }[]
}

/** GET /api/v1/auth/me: decides between setup, login and dashboard (spec 9.1). */
export interface Me {
  user: { id: number; name: string; via: 'session' | 'token' } | null
  setup_required: boolean
  public_read: boolean
}

export interface UserInfo {
  id: number
  name: string
  lang?: string
  created_at: string
  last_login: string | null
}

export interface TokenInfo {
  id: string
  name: string
  scope: string
  created_at: string
  expires_at: string | null
  last_used: string | null
}

/** A created token; `token` is shown only once. */
export interface NewToken extends TokenInfo {
  token: string
}

export interface AuditEntry {
  id: number
  at: string
  actor?: string
  action: string
  detail?: Record<string, unknown>
}

/** One UI setting with its effective value (spec 3). */
export interface SettingView {
  value: unknown
  locked: boolean
  stored: boolean
}

export interface ConfigObject {
  kind: 'connection' | 'device' | 'meter'
  id: string
  locked: boolean
  body: Record<string, unknown>
  updated_at: string | null
  updated_by?: string
  unreadable?: string[]
}

export interface ConfigOverview {
  config_rev: number
  objects: ConfigObject[]
  settings: Record<string, SettingView>
}

/** One object write of a batch; a null body deletes the object. */
export interface ConfigChange {
  kind: ConfigObject['kind']
  id: string
  body: Record<string, unknown> | null
}

/** A meter block of a profile with the roles it may take. */
interface ProfileBlock {
  name: string
  role: string
  roles_allowed: string[]
  category?: string
  channels: string[]
}

export interface ProfileInfo {
  id: string
  vendor: string
  models: string[]
  transport: 'modbus' | 'http'
  verified: boolean
  netting?: string
  blocks: ProfileBlock[]
}

export interface ProbeChannel {
  name: string
  value: number | null
  text?: string
  unit?: string
}

export interface ProbeBlock {
  name: string
  role: string
  roles_allowed: string[]
  category?: string
  channels: ProbeChannel[]
}

/** One read of a device with a draft configuration; error is set when it did not answer. */
export interface ProbeResult {
  device: string
  profile: string
  verified: boolean
  at: string
  took_ms: number
  error?: string
  blocks: ProbeBlock[]
}
