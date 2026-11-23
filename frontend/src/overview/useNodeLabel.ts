import { useI18n } from 'vue-i18n'
import { formatPercent, type Lang } from '@/format'
import type { RolePower } from './power'

/** Translated node label: "PV · 2 sources", "Grid · import", "Battery · 64 %", "Wallbox" (never a count of loadpoints or consumers). */
export function useNodeLabel() {
  const { t, locale } = useI18n()
  return (n: RolePower, soc?: number, socApprox = false): string => {
    const name = t(`roles.${n.kind}`)
    const p = n.power ?? 0
    switch (n.kind) {
      case 'pv':
        return n.count > 1 ? t('flow.pvSources', { name, n: n.count }) : name
      case 'grid':
        if (p > 0) return t('flow.detail', { name, detail: t('flow.import') })
        if (p < 0) return t('flow.detail', { name, detail: t('flow.export') })
        return name
      case 'battery': {
        if (soc === undefined) return name
        const pct = formatPercent(soc, locale.value as Lang)
        return t('flow.detail', { name, detail: socApprox ? `≈${pct}` : pct })
      }
      default:
        return name
    }
  }
}
