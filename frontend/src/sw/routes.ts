// Which strategy the service worker uses for a request (spec 11.11). Pure, so it is
// tested without a worker.

/** Header the worker adds to an API answer it kept: when that copy was fetched. */
export const cachedHeader = 'x-pvmon-cached-at'

/**
 * shell: a page navigation, answered with the accepted UI version's index.html;
 * asset: a built file (content-hashed or public), from the cache first;
 * api: a GET of the API, from the network first, the kept copy when offline;
 * network: everything the worker must not touch.
 */
export type Route = 'shell' | 'asset' | 'api' | 'network'

// The version must always be the server's own answer: the update banner compares it.
const networkOnly = ['/api/v1/version', '/health', '/ws', '/docs']

export function routeOf(url: URL, method: string, mode: string, origin: string): Route {
  if (method !== 'GET' || url.origin !== origin) return 'network'
  if (networkOnly.some((p) => url.pathname === p || url.pathname.startsWith(`${p}/`))) {
    return 'network'
  }
  if (url.pathname.startsWith('/api/')) {
    // Downloads (PDF, CSV) are never kept.
    return url.searchParams.has('format') ? 'network' : 'api'
  }
  return mode === 'navigate' ? 'shell' : 'asset'
}
