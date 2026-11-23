// Static i18n lookups for report ids, so every key stays visible to the unused-key lint.
import type { ComposerTranslation } from 'vue-i18n'

export function figureLabel(t: ComposerTranslation, id: string): string {
  switch (id) {
    case 'pv':
      return t('reports.fig.pv')
    case 'consumption':
      return t('reports.fig.consumption')
    case 'self_sufficiency':
      return t('reports.fig.selfSufficiency')
    case 'self_consumption':
      return t('reports.fig.selfConsumption')
    case 'grid_import':
      return t('reports.fig.gridImport')
    case 'grid_export':
      return t('reports.fig.gridExport')
    case 'battery_charge':
      return t('reports.fig.charge')
    case 'battery_discharge':
      return t('reports.fig.discharge')
    default:
      return id
  }
}

export function categoryLabel(t: ComposerTranslation, c: string | undefined): string {
  switch (c) {
    case 'wallbox':
      return t('roles.wallbox')
    case 'heatpump':
      return t('roles.heatpump')
    case 'heater':
      return t('roles.heater')
    default:
      return t('roles.other')
  }
}
