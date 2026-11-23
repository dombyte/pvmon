// The app side of the service worker (spec 11.11): registration and taking an update the
// user accepted. The worker never switches the UI by itself (see src/sw/sw.ts).

const askTimeoutMs = 5000
const installTimeoutMs = 30_000

interface WorkerInfo {
  build: string
  version: string
  accepted: string | null
}

export function registerWorker(): void {
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(() => undefined)
  }
}

function ask(w: ServiceWorker, type: 'info' | 'accept'): Promise<WorkerInfo> {
  return new Promise((resolve, reject) => {
    const ch = new MessageChannel()
    const timer = setTimeout(() => reject(new Error('service worker did not answer')), askTimeoutMs)
    ch.port1.onmessage = (e) => {
      clearTimeout(timer)
      resolve(e.data as WorkerInfo)
    }
    w.postMessage({ type }, [ch.port2])
  })
}

/** Resolves once a worker that is still installing has finished (or failed). */
function installed(w: ServiceWorker): Promise<void> {
  return new Promise((resolve) => {
    const done = () => ['installed', 'activated', 'redundant'].includes(w.state)
    if (done()) return resolve()
    const timer = setTimeout(resolve, installTimeoutMs)
    w.addEventListener('statechange', () => {
      if (done()) {
        clearTimeout(timer)
        resolve()
      }
    })
  })
}

/** "reloading": the new UI loads; "unavailable": no newer UI could be fetched yet. */
export type ApplyResult = 'reloading' | 'unavailable'

/**
 * Takes the update the user asked for: fetches the newest worker, checks it carries a UI
 * other than this one (else reloading would show the same old UI and the banner again),
 * makes it the served UI and reloads. Without a worker a reload gets the new UI directly.
 */
export async function applyUpdate(built: string): Promise<ApplyResult> {
  const reg =
    'serviceWorker' in navigator ? await navigator.serviceWorker.getRegistration() : undefined
  if (!reg?.active) {
    window.location.reload()
    return 'reloading'
  }
  try {
    await reg.update()
  } catch {
    // Offline: what is installed already is all there is.
  }
  if (reg.installing) await installed(reg.installing)
  const worker = reg.waiting ?? reg.active
  try {
    const info = await ask(worker, 'info')
    if (info.version === built && built !== 'dev') return 'unavailable'
    await ask(worker, 'accept')
  } catch {
    return 'unavailable'
  }
  window.location.reload()
  return 'reloading'
}
