// Icon, colour token and label of each flow node kind.
import {
  AirVent,
  BatteryMedium,
  Flame,
  House,
  Plug,
  PlugZap,
  Sun,
  Zap,
  type LucideIcon,
} from '@lucide/vue'
import type { NodeKind } from './power'

export const roleIcon: Record<NodeKind, LucideIcon> = {
  pv: Sun,
  grid: Zap,
  battery: BatteryMedium,
  house: House,
  wallbox: PlugZap,
  heatpump: AirVent,
  heater: Flame,
  other: Plug,
}

/** CSS variable of the role colour (Graphite tokens). */
export const roleColor: Record<NodeKind, string> = {
  pv: 'var(--pv)',
  grid: 'var(--grid)',
  battery: 'var(--battery)',
  house: 'var(--home)',
  wallbox: 'var(--ev)',
  heatpump: 'var(--heatpump)',
  heater: 'var(--heater)',
  other: 'var(--other)',
}
