// Whether the UI shows kept API answers (spec 11.11): the service worker marks a copy it
// answers offline with the time it was fetched; any fresh answer clears the mark.
import { ref } from 'vue'

/**
 * Header the worker sets on a kept answer (src/sw/routes.ts has the same; a test checks
 * they match). Not imported from there: the worker must not share modules with the app.
 */
export const cachedHeader = 'x-pvmon-cached-at'

/** Fetch time of the oldest kept answer on screen, null while online. */
export const offlineSince = ref<string | null>(null)

export function noteResponse(res: Response): void {
  const at = res.headers.get(cachedHeader)
  // Only a fresh answer clears it: an error offline says nothing about the data shown.
  if (at === null) {
    if (res.ok) offlineSince.value = null
  }
  else if (offlineSince.value === null || at < offlineSince.value) offlineSince.value = at
}
