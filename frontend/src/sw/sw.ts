// Service worker (spec 11.11): offline UI and the last API answers, without ever switching
// the UI version on its own.
//
// Every build precaches its files into its own cache "pvmon-shell-<build>". A pointer in
// "pvmon-meta" names the accepted build; pages are always answered with that build's
// index.html. A new build installs next to it and waits: it becomes the served UI only
// when the user accepts the update banner (message "accept"), also across app restarts.
// The banner itself compares the UI's version with the server's /api/v1/version, which
// this worker never answers from a cache. First installs and development builds take over
// at once (there is nothing to ask about).
//
// Built as its own entry: the vite plugin in vite.config.ts fills in the build id and the
// file list. It must not share modules with the app (it runs as a classic script).
import { cachedHeader, routeOf } from './routes'

declare const __PVMON_BUILD__: string
declare const __PVMON_PRECACHE__: string[]

interface ExtendableEvent extends Event {
  waitUntil(p: Promise<unknown>): void
}
interface FetchEvent extends ExtendableEvent {
  request: Request
  respondWith(r: Promise<Response>): void
}
interface MessageEventSW extends ExtendableEvent {
  data: unknown
  ports: readonly MessagePort[]
}
interface Scope {
  location: Location
  skipWaiting(): Promise<void>
  clients: { claim(): Promise<void> }
  addEventListener(type: 'install' | 'activate', f: (e: ExtendableEvent) => void): void
  addEventListener(type: 'fetch', f: (e: FetchEvent) => void): void
  addEventListener(type: 'message', f: (e: MessageEventSW) => void): void
}

const sw = self as unknown as Scope
const build = __PVMON_BUILD__
const version: string = import.meta.env.VITE_APP_VERSION || 'dev'
const precache = __PVMON_PRECACHE__
const shellPrefix = 'pvmon-shell-'
const apiCache = 'pvmon-api'
const metaCache = 'pvmon-meta'
const pointerKey = '/__pvmon/current'
const indexPath = '/index.html'
const apiTimeoutMs = 6000
const apiEntries = 300
// Requests differ in headers (Origin, Accept) from the precache: match by URL only.
const byURL = { ignoreVary: true }

async function accepted(): Promise<string | null> {
  const r = await (await caches.open(metaCache)).match(pointerKey)
  return r ? r.text() : null
}

async function accept(b: string): Promise<void> {
  await (await caches.open(metaCache)).put(pointerKey, new Response(b))
}

/** Drops the shells of builds that are neither accepted nor this one. */
async function cleanup(): Promise<void> {
  const keep = new Set([shellPrefix + build, shellPrefix + ((await accepted()) ?? build)])
  const names = await caches.keys()
  await Promise.all(
    names.filter((n) => n.startsWith(shellPrefix) && !keep.has(n)).map((n) => caches.delete(n)),
  )
}

sw.addEventListener('install', (e) => {
  e.waitUntil(
    (async () => {
      const cache = await caches.open(shellPrefix + build)
      await cache.addAll(precache.map((u) => new Request(u, { cache: 'reload' })))
      const cur = await accepted()
      const lost = cur !== null && !(await caches.has(shellPrefix + cur))
      if (cur === null || cur === build || lost || version === 'dev') {
        await accept(build)
        await sw.skipWaiting()
      }
    })(),
  )
})

sw.addEventListener('activate', (e) => {
  e.waitUntil(cleanup().then(() => sw.clients.claim()))
})

/** A page: the accepted build's index.html; the network only when that is missing. */
async function shell(req: Request): Promise<Response> {
  const cur = await accepted()
  const hit = cur ? await (await caches.open(shellPrefix + cur)).match(indexPath, byURL) : undefined
  if (hit) return hit
  try {
    return await fetch(req)
  } catch {
    return (await caches.match(indexPath, byURL)) ?? Response.error()
  }
}

/** A built file: content-hashed names never change, so any cache that has it is right. */
async function asset(req: Request): Promise<Response> {
  return (await caches.match(req, { ...byURL, ignoreSearch: true })) ?? fetch(req)
}

async function stamped(res: Response): Promise<Response> {
  const headers = new Headers(res.headers)
  headers.set(cachedHeader, new Date().toISOString())
  return new Response(await res.blob(), { status: res.status, statusText: res.statusText, headers })
}

async function trim(cache: Cache): Promise<void> {
  const keys = await cache.keys()
  await Promise.all(keys.slice(0, Math.max(0, keys.length - apiEntries)).map((k) => cache.delete(k)))
}

/** API: the network first; offline (or no answer in time) the kept copy, marked as such. */
async function api(req: Request): Promise<Response> {
  const cache = await caches.open(apiCache)
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), apiTimeoutMs)
  try {
    const res = await fetch(req, { signal: ctrl.signal })
    if (res.ok) {
      await cache.put(req, await stamped(res.clone()))
      void trim(cache)
    }
    return res
  } catch {
    const hit = await cache.match(req, byURL)
    if (hit) return hit
    const body = { error: 'Service Unavailable', code: 'network', message: 'offline' }
    return new Response(JSON.stringify(body), {
      status: 503,
      headers: { 'Content-Type': 'application/json' },
    })
  } finally {
    clearTimeout(timer)
  }
}

sw.addEventListener('fetch', (e) => {
  const route = routeOf(new URL(e.request.url), e.request.method, e.request.mode, sw.location.origin)
  if (route === 'shell') e.respondWith(shell(e.request))
  else if (route === 'asset') e.respondWith(asset(e.request))
  else if (route === 'api') e.respondWith(api(e.request))
})

/** "info" answers which build this is; "accept" makes this build the served UI. */
sw.addEventListener('message', (e) => {
  const type = (e.data as { type?: string } | null)?.type
  e.waitUntil(
    (async () => {
      if (type === 'accept') {
        await accept(build)
        await cleanup()
        await sw.skipWaiting()
      }
      e.ports[0]?.postMessage({ build, version, accepted: await accepted() })
    })(),
  )
})
