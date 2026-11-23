import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { downloadReport, errorKey, fetchReport, type ReportQuery } from '@/api/client'
import { usePolled } from '@/composables/usePolled'
import { todayIn } from '@/format/localdate'
import { useSiteStore } from '@/stores/site'
import { reportQuery } from './period'
import { saveBlob } from '@/composables/saveBlob'

/** The report of the route's period, and downloads of it as PDF or CSV. */
export function useReport(lang: () => string) {
  const route = useRoute()
  const router = useRouter()
  const site = useSiteStore()
  const today = computed(() => (site.model ? todayIn(site.model.tz) : ''))
  const query = computed(() => (today.value ? reportQuery(route.query, today.value) : null))
  const { data, error } = usePolled(query, fetchReport, 0)

  function update(next: Partial<ReportQuery>): void {
    if (!query.value) return
    void router.replace({ query: { ...query.value, ...next } })
  }

  const downloading = ref<'pdf' | 'csv' | null>(null)
  const downloadError = ref<string | null>(null)
  async function download(format: 'pdf' | 'csv'): Promise<void> {
    if (!query.value) return
    downloading.value = format
    downloadError.value = null
    try {
      const file = await downloadReport(query.value, format, lang())
      saveBlob(file.blob, file.name)
    } catch (err) {
      downloadError.value = errorKey(err)
    } finally {
      downloading.value = null
    }
  }
  return {
    today,
    query,
    report: data,
    error,
    update,
    download,
    downloading,
    downloadError,
  }
}
