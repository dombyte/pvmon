import { readFileSync } from 'node:fs'
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { SiteModel } from '../src/api/types'
import { pvGrid } from '../src/overview/fixtures'
import { mockServer } from './mock'

// Gate 2: the overview renders from /api/v1/site for R1, R3 and R4 (the bodies the Go
// replay test pins in internal/app/testdata) and PV-only, light and dark, desktop and
// phone (projects), and passes axe (spec 15).
const golden = (name: string): SiteModel =>
  JSON.parse(
    readFileSync(new URL(`../../internal/app/testdata/site-${name}.json`, import.meta.url), 'utf8'),
  ) as SiteModel
const online = (s: SiteModel): SiteModel => ({
  ...s,
  name: 'Zuhause',
  data_since: '2025-03-01',
  devices: [...new Set(s.meters.map((m) => m.device))].map((id) => ({
    id,
    profile: 'test/profile',
    verified: true,
    state: 'online',
  })),
})

const setups: Record<string, SiteModel> = {
  r1: online(golden('r1')),
  r3: online(golden('r3')),
  r4: online(golden('r4')),
  'pv-only': online(pvGrid),
}

for (const [name, site] of Object.entries(setups)) {
  for (const scheme of ['light', 'dark'] as const) {
    test(`overview ${name} ${scheme}`, async ({ page }, info) => {
      await page.emulateMedia({ colorScheme: scheme })
      await mockServer(page, site)
      await page.goto('/')
      await expect(page.getByRole('heading', { name: 'Übersicht', level: 1 })).toBeVisible()
      // Live values, energy and (desktop) the lazy chart have arrived.
      await expect(page.getByText(/Stand 13:42:05/)).toBeVisible()
      await expect(page.getByText('Heute', { exact: true }).first()).toBeVisible()
      if (info.project.name === 'desktop') {
        await expect(page.locator('[role="img"] svg').last()).toBeVisible()
      } else {
        await expect(page.getByText('Heute (läuft)')).toBeVisible()
      }

      // The shell scrolls inside <main>, so grow the viewport until main shows everything.
      await page.evaluate(() => document.fonts.ready)
      const size = page.viewportSize() ?? { width: 0, height: 0 }
      const overflow = await page.locator('main').evaluate((m) => m.scrollHeight - m.clientHeight)
      await page.setViewportSize({ width: size.width, height: size.height + overflow })
      await expect(page).toHaveScreenshot(`overview-${name}-${scheme}.png`)

      const axe = await new AxeBuilder({ page }).analyze()
      const found = axe.violations.flatMap((v) =>
        v.nodes.map((n) => `${v.id}: ${n.html} ${n.failureSummary ?? ''}`),
      )
      expect(found).toEqual([])
    })
  }
}

test('trend pins a day with its links', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'the trend chart is a desktop widget')
  await mockServer(page, setups.r4!)
  await page.goto('/')
  const chart = page.locator('[role="img"] svg').last()
  await expect(chart).toBeVisible()
  const box = (await chart.boundingBox())!
  await page.mouse.click(box.x + box.width * 0.5, box.y + box.height * 0.5)
  const report = page.getByRole('link', { name: 'Tagesreport öffnen' })
  await expect(report).toBeVisible()
  await page.mouse.move(0, 0)
  await expect(report).toBeVisible()
  await page.getByRole('button', { name: 'Auswahl aufheben' }).click()
  await expect(report).toHaveCount(0)
})

test('phone bar holds the daily pages, the rest is under More', async ({ page }, info) => {
  test.skip(info.project.name !== 'phone', 'compact layout only')
  await mockServer(page, setups.r4!)
  await page.goto('/')
  const bar = page.getByRole('navigation', { name: 'Navigation', exact: true })
  await expect(bar.getByRole('link')).toHaveText(['Übersicht', 'Tagesverlauf', 'Verlauf', 'Berichte', 'Mehr'])
  await expect(page.locator('header')).toHaveCount(0)
  await bar.getByRole('link', { name: 'Mehr' }).click()
  await page.getByRole('link', { name: 'Geräte' }).click()
  await expect(page.getByRole('heading', { name: 'Geräte', level: 1 })).toBeVisible()
  await expect(bar.getByRole('link', { name: 'Mehr' })).toHaveAttribute('aria-current', 'page')
  await bar.getByRole('link', { name: 'Mehr' }).click()
  await page.getByRole('button', { name: /^Sprache: Deutsch/ }).click()
  await page.getByRole('option', { name: 'English' }).click()
  await expect(page.getByRole('heading', { name: 'More', level: 1 })).toBeVisible()
})

test('power now folds to bars and opens a breakdown without a focus ring', async ({ page }, info) => {
  test.skip(info.project.name !== 'phone', 'compact layout only')
  await mockServer(page, setups.r4!)
  await page.goto('/')
  const card = page.locator('section').filter({ hasText: 'Leistung jetzt' })
  await expect(card.getByRole('button', { name: /^Wallbox \d/ })).toHaveCount(2) // bar and list
  await expect(card.getByText(/×/)).toHaveCount(0)
  const tall = (await card.boundingBox())!.height

  // A breakdown: the dialog holds the focus, not its close button.
  await card.getByRole('button', { name: /^Wallbox \d/ }).last().tap()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Schließen' })).not.toBeFocused()
  await dialog.getByRole('button', { name: 'Schließen' }).tap()

  // Battery and grid spell out their direction instead of a sign.
  await card.getByRole('button', { name: /Ladestand der Batterie/ }).tap()
  await expect(dialog.getByRole('definition').first()).toHaveText(/^entlädt\s+1,2\s*kW$/)
  await expect(dialog.getByRole('definition').filter({ hasText: /−|-\d/ })).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Schließen' }).tap()

  // A bar segment opens its breakdown too, and does not fold the card.
  await card.getByRole('button', { name: /^PV \d/ }).first().tap()
  await expect(dialog.getByRole('heading', { name: 'PV' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Schließen' }).tap()
  await expect(card.getByText('Wallbox', { exact: true }).first()).toBeVisible()

  // Only the title button folds: row titles and the battery chip stay, values go.
  const fold = card.getByRole('button', { name: 'Leistung jetzt: nur Balken' })
  await expect(fold).toHaveAttribute('aria-expanded', 'true')
  await fold.tap()
  const unfold = card.getByRole('button', { name: 'Leistung jetzt: Werte zeigen' })
  await expect(unfold).toHaveAttribute('aria-expanded', 'false')
  await expect(card.getByText('Woher')).toBeVisible()
  await expect(card.getByText('Wohin')).toBeVisible()
  await expect(card.getByText('5,5 kW').first()).toBeHidden()
  await expect(card.getByRole('button', { name: /^Wallbox \d/ })).toHaveCount(1) // the segment
  await expect(card.getByRole('button', { name: /Ladestand der Batterie/ })).toBeVisible()
  await expect.poll(async () => (await card.boundingBox())!.height).toBeLessThan(tall - 40)
  await page.reload()
  await expect(unfold).toBeVisible()
  await unfold.tap()
  await expect(card.getByText('5,5 kW').first()).toBeVisible()
})

test('manual update check answers in place', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'the sidebar card')
  let server = '1.0.0-e2e'
  await mockServer(page, setups.r4!, { version: () => server })
  await page.goto('/')
  const check = page.getByRole('button', { name: 'Nach Updates suchen' })
  await check.click()
  await expect(page.getByRole('status').filter({ hasText: 'ist aktuell' })).toBeVisible()
  await expect(page.getByText(/^Neue Version/)).toHaveCount(0)
  // The server was updated: the check says so and the banner appears, dismissible.
  server = '1.1.0-e2e'
  await check.click()
  await expect(page.getByRole('button', { name: 'Neue Version 1.1.0-e2e – neu laden' })).toBeVisible()
  await expect(page.getByText('Neue Version 1.1.0-e2e verfügbar.')).toBeVisible()
  await page.getByRole('button', { name: 'Hinweis schließen' }).click()
  await expect(page.getByText('Neue Version 1.1.0-e2e verfügbar.')).toHaveCount(0)
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Übersicht', level: 1 })).toBeVisible()
  await expect(page.getByText('Neue Version 1.1.0-e2e verfügbar.')).toHaveCount(0)
})


// A notched phone (iPhone 15: 393 × 852, insets 59 / 34). Chromium has no real insets, so
// the test sets the inset variables and draws the island and the home indicator.
for (const path of ['/', '/day?date=2026-11-18', '/more']) {
  test(`notch ${path}`, async ({ page }, info) => {
    test.skip(info.project.name !== 'phone', 'compact layout only')
    await page.setViewportSize({ width: 393, height: 852 })
    await mockServer(page, setups.r4!)
    await page.goto(path)
    await expect(page.locator('h1')).toBeVisible()
    if (path.startsWith('/day')) {
      await expect(page.getByText('96 von 96 Viertelstunden vollständig')).toBeVisible()
      await expect(page.locator('[role="img"] svg')).toHaveCount(3)
    }
    await page.addStyleTag({
      content: `:root { --safe-top: 59px; --safe-bottom: 34px; }
        body::before, body::after { content: ''; position: fixed; z-index: 99; left: 50%;
          transform: translateX(-50%); background: #000; border-radius: 99px; }
        body::before { top: 11px; width: 126px; height: 37px; }
        body::after { bottom: 8px; width: 134px; height: 5px; }`,
    })
    await page.evaluate(() => document.fonts.ready)
    await page.waitForTimeout(300)
    await expect(page).toHaveScreenshot(`notch-${path === '/' ? 'overview' : path.replace(/^\//, '').replace(/[?=&]+/g, '-')}.png`)
  })
}
