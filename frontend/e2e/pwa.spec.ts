import { readFileSync, writeFileSync } from 'node:fs'
import { expect, test, type BrowserContext, type Page } from '@playwright/test'
import { r1 } from '../src/overview/fixtures'
import { mockServer, uiVersion } from './mock'

// Spec 11.11: the PWA is installable, works offline with the last API answers, and a new
// UI build never replaces the accepted one before the user accepts the update banner,
// not on reload and not on an app restart.
test.use({ serviceWorkers: 'allow' })
test.skip(({ isMobile }) => isMobile, 'one browser run is enough')
// One worker: the update test swaps the served sw.js.
test.describe.configure({ mode: 'serial' })

const workerFile = 'dist/sw.js'

interface WorkerInfo {
  build: string
  version: string
  accepted: string
}

/** Asks the controlling worker which build it is and which one it serves. */
async function info(page: Page): Promise<WorkerInfo> {
  for (;;) {
    try {
      return await ask(page)
    } catch (err) {
      // A reload was still under way; ask the new page.
      if (!String(err).includes('context was destroyed')) throw err
      await page.waitForLoadState()
    }
  }
}

function ask(page: Page): Promise<WorkerInfo> {
  return page.evaluate(
    () =>
      new Promise<WorkerInfo>((resolve) => {
        const ch = new MessageChannel()
        ch.port1.onmessage = (e) => resolve(e.data as WorkerInfo)
        navigator.serviceWorker.controller!.postMessage({ type: 'info' }, [ch.port2])
      }),
  )
}

async function controlled(page: Page): Promise<void> {
  await page.evaluate(() => navigator.serviceWorker.ready.then(() => true))
  await page.reload()
  await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true)
}

test('pwa is installable', async ({ page, context }) => {
  await mockServer(page, r1, { target: context })
  await page.goto('/')
  await page.evaluate(() => navigator.serviceWorker.ready.then(() => true))

  const cdp = await context.newCDPSession(page)
  const { installabilityErrors } = await cdp.send('Page.getInstallabilityErrors')
  expect(installabilityErrors).toEqual([])

  const manifest = (await (await page.request.get('/manifest.webmanifest')).json()) as {
    display: string
    icons: { sizes: string; purpose: string }[]
    shortcuts: { url: string }[]
  }
  expect(manifest.display).toBe('standalone')
  const icons = manifest.icons.map((i) => `${i.sizes} ${i.purpose}`)
  expect(icons).toEqual(
    expect.arrayContaining(['192x192 any', '512x512 any', '192x192 maskable', '512x512 maskable']),
  )
  expect(manifest.shortcuts.map((s) => s.url)).toEqual(['/day', '/history', '/reports', '/status'])
  await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveCount(1)
})

test('pwa shows the last data offline', async ({ page, context }) => {
  let offline = false
  await mockServer(page, r1, { target: context })
  await context.route('**', (r) => (offline ? r.abort('internetdisconnected') : r.fallback()))
  await page.goto('/')
  await controlled(page)
  await expect(page.getByText('Stand 13:42:05')).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Offline' })).toHaveCount(0)

  offline = true
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Übersicht', level: 1 })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Offline: Daten vom' })).toBeVisible()
  // The version is never answered from a cache: offline there is no banner.
  await expect(page.getByText(/Neue Version/)).toHaveCount(0)

  offline = false
  await page.reload()
  await expect(page.getByText('Stand 13:42:05')).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'Offline' })).toHaveCount(0)
})

test('a new build waits for the user, also across an app restart', async ({ page, context }) => {
  let server = uiVersion
  await mockServer(page, r1, { target: context, version: () => server })
  // A worker's update check passes Playwright's routes by, so the "new build" is written
  // into the served file: the same files under another build id and version, same length
  // (the preview server keeps the file sizes it saw at start).
  const original = readFileSync(workerFile, 'utf8')
  const next = original.replace(/"[0-9a-f]{12}"/, '"0123456789ab"').replaceAll(uiVersion, '1.1.0-e2e')
  try {
    await updateFlow(page, context, () => writeFileSync(workerFile, next), (v) => (server = v))
  } finally {
    writeFileSync(workerFile, original)
  }
})

async function updateFlow(
  page: Page,
  context: BrowserContext,
  publish: () => void,
  serverRuns: (v: string) => void,
): Promise<void> {
  await page.goto('/')
  await controlled(page)
  const old = await info(page)
  expect(old).toMatchObject({ version: uiVersion, accepted: old.build })

  // The new worker installs and waits; a reload keeps the accepted UI.
  publish()
  await page.evaluate(() => navigator.serviceWorker.getRegistration().then((r) => r?.update()))
  await expect
    .poll(() => page.evaluate(async () => !!(await navigator.serviceWorker.getRegistration())?.waiting))
    .toBe(true)
  await page.reload()
  expect(await info(page)).toMatchObject({ build: old.build, accepted: old.build })

  // App restart: every page closed, the new worker activates, the old UI is still served.
  await page.close()
  // The waiting worker activates once no page uses the old one; that is asynchronous, so a
  // start that came too early leaves (about:blank) and starts again, as a user would.
  const again = await context.newPage()
  await expect
    .poll(async () => {
      await again.goto('/')
      const { build } = await info(again)
      if (build !== '0123456789ab') await again.goto('about:blank')
      return build
    })
    .toBe('0123456789ab')
  await expect(again.getByRole('heading', { name: 'Übersicht', level: 1 })).toBeVisible()
  expect((await info(again)).accepted).toBe(old.build)
  await expect(again.getByText(/Neue Version/)).toHaveCount(0)

  // The server runs the new version: the banner asks, and only "reload" switches.
  serverRuns('1.1.0-e2e')
  await again.getByRole('button', { name: 'Nach Updates suchen' }).click()
  await expect(again.getByText('Neue Version 1.1.0-e2e verfügbar.')).toBeVisible()
  await again.getByRole('button', { name: 'Neue Version 1.1.0-e2e – neu laden' }).click()
  await expect.poll(async () => (await info(again)).accepted).toBe('0123456789ab')
}
