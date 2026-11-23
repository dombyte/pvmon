import { useI18n } from 'vue-i18n'

/** Role and category names; keys spelled out for the i18n key check. */
export function useRoleLabel() {
  const { t } = useI18n()
  const labels: Record<string, () => string> = {
    pv: () => t('roles.pv'),
    grid: () => t('roles.grid'),
    battery: () => t('roles.battery'),
    backup: () => t('roles.backup'),
    consumer: () => t('roles.consumer'),
    inverter: () => t('roles.inverter'),
    phase: () => t('wizard.meters.phase'),
    wallbox: () => t('roles.wallbox'),
    heatpump: () => t('roles.heatpump'),
    heater: () => t('roles.heater'),
    other: () => t('roles.other'),
  }
  return (role: string) => labels[role]?.() ?? role
}

export const categories = ['wallbox', 'heatpump', 'heater', 'other'] as const
