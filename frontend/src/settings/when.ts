import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime, missing, type Lang } from '@/format'
import { useSiteStore } from '@/stores/site'

/** Formats server instants in the site's timezone (the browser's until the site loaded). */
export function useWhen() {
  const { locale } = useI18n()
  const site = useSiteStore()
  const tz = computed(
    () => site.model?.tz ?? Intl.DateTimeFormat().resolvedOptions().timeZone,
  )
  return (at: string | null | undefined): string =>
    at ? formatDateTime(at, locale.value as Lang, tz.value) : missing
}
