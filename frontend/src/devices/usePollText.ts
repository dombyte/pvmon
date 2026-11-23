import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DeviceInfo } from '@/api/types'
import { formatNumber, type Lang } from '@/format'

/** "312 ms" or "failed after 2,000 ms" for the newest poll of a device. */
export function usePollText() {
  const { t, locale } = useI18n()
  const lang = computed(() => locale.value as Lang)
  return (p: NonNullable<DeviceInfo['last_poll']>) => {
    const ms = formatNumber(p.took_ms, lang.value, 0)
    return p.ok ? t('devices.took', { ms }) : t('devices.failedAfter', { ms })
  }
}
