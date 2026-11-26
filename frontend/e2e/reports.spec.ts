import { readFileSync } from 'node:fs'
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import type { SiteModel } from '../src/api/types'
import { mockServer } from './mock'

// Reports page (spec 11, 12) for R4, light and dark, and axe.
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
  await page.setViewportSize({
    width: size.width,
    height: size.height + overflow,
  })
  await expect(page).toHaveScreenshot(name)
  const axe = await new AxeBuilder({ page }).analyze()
  const found = axe.violations.flatMap((v) =>
    v.nodes.map((n) => `${v.id}: ${n.html} ${n.failureSummary ?? ''}`),
  )
  expect(found).toEqual([])
}

for (const scheme of ['light', 'dark'] as const) {
  test(`reports ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site)
    await page.goto('/reports?from=2026-10-01&to=2026-10-31&compare=last_year')
    await expect(page.getByText('414,2 kWh').first()).toBeVisible()
    await expect(page.getByText('+8,9 %')).toBeVisible()
    await shoot(page, `reports-${scheme}.png`)
  })
}

test('report controls and downloads', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/reports?from=2026-10-01&to=2026-10-31')
  await page.getByRole('button', { name: 'Letztes Jahr' }).click()
  await expect(page).toHaveURL(/from=2025-01-01&to=2025-12-31/)
  // The themed select: opens a listbox, keyboard and pointer both choose.
  await page.getByRole('button', { name: 'Vergleich: Vorjahr' }).click()
  const list = page.getByRole('listbox', { name: 'Vergleich' })
  await expect(list.getByRole('option', { name: 'Vorjahr' })).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('Escape')
  await expect(list).toHaveCount(0)
  await page.getByRole('button', { name: 'Vergleich: Vorjahr' }).press('ArrowDown')
  await page.keyboard.press('End')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/compare=none/)
  await expect(page.getByText('Verglichen mit')).toHaveCount(0)
  // The period table is folded until asked for.
  const table = page.getByRole('button', { name: /^Tabelle/ })
  await expect(table).toHaveAttribute('aria-expanded', 'false')
  await table.click()
  await expect(page.getByRole('region', { name: 'Tabelle' })).toBeVisible()
  // Dates come from the themed calendar.
  await page.getByRole('button', { name: /^Zeitraum:/ }).click()
  const range = page.getByRole('dialog', { name: 'Zeitraum' })
  for (let i = 0; i < 10; i++) await range.getByRole('button', { name: 'Vorheriger Monat' }).click()
  await range.getByRole('button', { name: 'Montag, 3. Februar 2025' }).click()
  await range.getByRole('button', { name: 'Freitag, 7. Februar 2025' }).click()
  await expect(page).toHaveURL(/from=2025-02-03&to=2025-02-07/)
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'CSV' }).click()
  expect((await download).suggestedFilename()).toBe('pvmon-report-2026-10-01_2026-10-31.csv')
  await page.getByRole('button', { name: 'PDF' }).click()
  await expect(page.getByRole('alert')).toHaveText('Interner Serverfehler.')
})
