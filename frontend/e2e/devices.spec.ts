import { readFileSync } from 'node:fs'
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import type { SiteModel } from '../src/api/types'
import { mockServer } from './mock'

// Devices, device detail and status (spec 11.3) for R4, light and dark, and axe.
const site = {
  ...(JSON.parse(
    readFileSync(new URL('../../internal/app/testdata/site-r4.json', import.meta.url), 'utf8'),
  ) as SiteModel),
  data_since: '2025-03-01',
}

async function shoot(page: Page, name: string): Promise<void> {
  await page.evaluate(() => document.fonts.ready)
  const size = page.viewportSize() ?? { width: 0, height: 0 }
  const overflow = await page.locator('main').evaluate((m) => m.scrollHeight - m.clientHeight)
  await page.setViewportSize({ width: size.width, height: size.height + overflow })
  await expect(page).toHaveScreenshot(name)
  const axe = await new AxeBuilder({ page }).analyze()
  const found = axe.violations.flatMap((v) =>
    v.nodes.map((n) => `${v.id}: ${n.html} ${n.failureSummary ?? ''}`),
  )
  expect(found).toEqual([])
}

for (const scheme of ['light', 'dark'] as const) {
  test(`devices ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site)
    await page.goto('/devices')
    await expect(page.getByRole('heading', { name: 'wr1' })).toBeVisible()
    await expect(page.getByText('Offline')).toBeVisible()
    await shoot(page, `devices-${scheme}.png`)
  })

  test(`device detail ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site)
    await page.goto('/devices/wr1')
    await expect(page.getByRole('heading', { name: 'wr1', level: 1 })).toBeVisible()
    await expect(page.getByText('Netzüberspannung')).toBeVisible()
    await expect(page.getByText('2,4 kW').first()).toBeVisible()
    await shoot(page, `device-${scheme}.png`)
  })

  test(`status ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site)
    await page.goto('/status')
    await expect(page.getByRole('link', { name: 'wr1', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: /5\. Nov\.: Störung/ }).first()).toBeVisible()
    await shoot(page, `status-${scheme}.png`)
  })
}

test('device list links to the detail', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/devices')
  await page.getByRole('link', { name: /wr1/ }).click()
  await expect(page).toHaveURL(/\/devices\/wr1$/)
  await page.goto('/devices/nope')
  await expect(page.getByText('Nicht gefunden.')).toBeVisible()
})

test('a status day opens the device with that day\'s messages', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/status')
  await page.getByRole('link', { name: /5\. Nov\.: Störung/ }).first().click()
  await expect(page).toHaveURL(/\/devices\/wr1\?day=2026-11-05#status$/)
  await expect(page.getByRole('heading', { name: /Meldungen am/ })).toBeVisible()
  await expect(page.getByText('Netzüberspannung')).toBeVisible()
  await expect(page.getByText('Wartend')).toHaveCount(0)
  // Hiding a register removes its row and its messages.
  await page.getByRole('button', { name: 'Netzfehler', exact: true }).click()
  await expect(page.getByText('Netzüberspannung')).toHaveCount(0)
  await page.getByRole('button', { name: 'Alle Meldungen' }).click()
  await expect(page).toHaveURL(/\/devices\/wr1#status$/)
})

test('status bars tell how long each state lasted', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'hover needs a pointer')
  await mockServer(page, site)
  await page.goto('/status')
  const bar = page.getByRole('link', { name: /^5\. Nov\.: Störung \(/ }).first()
  await expect(bar).toHaveAccessibleName(/Störung \d/)
  await bar.hover()
  await expect(page.getByText('Do., 5. Nov.')).toBeVisible()
  await page.mouse.move(0, 0)
  await expect(page.getByText('Do., 5. Nov.')).toHaveCount(0)
})

test('devices show the interval and the last poll', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/devices')
  await expect(page.getByText('alle 5 s · 312 ms').first()).toBeVisible()
  await expect(page.getByText('alle 5 s · fehlgeschlagen nach 2.003 ms')).toBeVisible()
  await page.goto('/devices/wr1')
  await expect(page.getByText('312 ms', { exact: true })).toBeVisible()
})

