import { readFileSync } from 'node:fs'
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { SiteModel } from '../src/api/types'
import { mockServer } from './mock'

// History (spec 11.8): 30 days of R4 with one day without data, light and dark, desktop
// and phone (projects), and axe.
const site = {
  ...(JSON.parse(
    readFileSync(new URL('../../internal/app/testdata/site-r4.json', import.meta.url), 'utf8'),
  ) as SiteModel),
  data_since: '2025-03-01',
}

for (const scheme of ['light', 'dark'] as const) {
  test(`history ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site)
    await page.goto('/history')
    await expect(page.getByRole('heading', { name: 'Verlauf', level: 1 })).toBeVisible()
    await expect(page.getByRole('button', { name: /21\. Okt\. – 19\. Nov\. 2026/ })).toBeVisible()
    await expect(page.getByText('ohne Daten: 1 von 30')).toBeVisible()
    await expect(page.locator('[role="img"] svg')).toBeVisible()

    await page.evaluate(() => document.fonts.ready)
    const size = page.viewportSize() ?? { width: 0, height: 0 }
    const overflow = await page.locator('main').evaluate((m) => m.scrollHeight - m.clientHeight)
    await page.setViewportSize({ width: size.width, height: size.height + overflow })
    await expect(page).toHaveScreenshot(`history-${scheme}.png`)

    const axe = await new AxeBuilder({ page }).analyze()
    const found = axe.violations.flatMap((v) =>
      v.nodes.map((n) => `${v.id}: ${n.html} ${n.failureSummary ?? ''}`),
    )
    expect(found).toEqual([])
  })
}

test('history switches granularity and range', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/history')
  await page.getByRole('button', { name: /21\. Okt\./ }).click()
  await page.getByRole('button', { name: 'Letzte 7 Tage' }).click()
  await expect(page.getByRole('button', { name: /13\. Nov\. – 19\. Nov\. 2026/ })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Nächster Zeitraum' })).toBeDisabled()
  await page.getByRole('button', { name: 'Vorheriger Zeitraum' }).click()
  await expect(page.getByRole('button', { name: /6\. Nov\. – 12\. Nov\. 2026/ })).toBeVisible()
  await page.getByRole('tab', { name: 'Jahre' }).click()
  await expect(page.getByRole('button', { name: '2022 – 2026' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('tab', { name: 'Jahre' })).toHaveAttribute('aria-selected', 'true')
})

test('history takes a custom range from the calendar', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/history')
  await page.getByRole('button', { name: /21\. Okt\./ }).click()
  await page.getByRole('button', { name: /^Von:/ }).click()
  const from = page.getByRole('dialog', { name: 'Von' })
  await from.getByRole('button', { name: 'Donnerstag, 1. Oktober 2026' }).click()
  await page.getByRole('button', { name: /^Bis:/ }).click()
  const to = page.getByRole('dialog', { name: 'Bis' })
  await to.getByRole('button', { name: 'Vorheriger Monat' }).click()
  await expect(to.getByText('Oktober 2026')).toBeVisible()
  await to.getByRole('button', { name: 'Samstag, 10. Oktober 2026' }).click()
  await page.getByRole('button', { name: 'Anwenden' }).click()
  await expect(page.getByRole('button', { name: /1\. Okt\. – 10\. Okt\. 2026/ })).toBeVisible()
})

test('months and years are picked as months and years', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/history')
  await page.getByRole('tab', { name: 'Monate' }).click()
  await page.getByRole('button', { name: /Nov\. 2026/ }).first().click()
  await page.getByRole('button', { name: /^Von:/ }).click()
  const from = page.getByRole('dialog', { name: 'Von' })
  await expect(from.getByRole('group').getByRole('button')).toHaveCount(12)
  await expect(from.getByRole('button', { name: /Donnerstag/ })).toHaveCount(0)
  if (await from.getByRole('button', { name: 'Juni 2026' }).isVisible()) {
    await from.getByRole('button', { name: 'Vorheriges Jahr' }).click()
  }
  await expect(from.getByRole('button', { name: 'Februar 2025' })).toBeDisabled() // no data yet
  await from.getByRole('button', { name: 'Juni 2025' }).click()
  await page.getByRole('button', { name: 'Anwenden' }).click()
  await expect(page.getByRole('button', { name: /Juni 2025 – Nov\. 2026|Jun\. 2025 – Nov\. 2026/ })).toBeVisible()

  await page.getByRole('tab', { name: 'Jahre' }).click()
  await page.getByRole('button', { name: /2026/ }).first().click()
  await page.getByRole('button', { name: /^Von:/ }).click()
  const years = page.getByRole('dialog', { name: 'Von' })
  await expect(years.getByRole('button', { name: '2025', exact: true })).toBeEnabled()
  await expect(years.getByRole('button', { name: '2024', exact: true })).toBeDisabled()
  await expect(years.getByRole('button', { name: '2027', exact: true })).toBeDisabled()
  await expect(years.getByText('2016 – 2027')).toBeVisible()
})
