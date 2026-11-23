import { readFileSync } from 'node:fs'
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { SiteModel } from '../src/api/types'
import { mockServer } from './mock'

// Day view (spec 11.3): yesterday of R4, light and dark, desktop and phone, and axe.
const site = {
  ...(JSON.parse(
    readFileSync(new URL('../../internal/app/testdata/site-r4.json', import.meta.url), 'utf8'),
  ) as SiteModel),
  data_since: '2025-03-01',
}

for (const scheme of ['light', 'dark'] as const) {
  test(`day ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site)
    await page.goto('/day?date=2026-11-18')
    await expect(page.getByRole('heading', { name: 'Tagesverlauf', level: 1 })).toBeVisible()
    await expect(page.getByText('Mi., 18. Nov. 2026', { exact: true })).toBeVisible()
    await expect(page.getByText('96 von 96 Viertelstunden vollständig')).toBeVisible()
    await expect(page.locator('[role="img"] svg')).toHaveCount(3)

    await page.evaluate(() => document.fonts.ready)
    const size = page.viewportSize() ?? { width: 0, height: 0 }
    const overflow = await page.locator('main').evaluate((m) => m.scrollHeight - m.clientHeight)
    await page.setViewportSize({ width: size.width, height: size.height + overflow })
    await expect(page).toHaveScreenshot(`day-${scheme}.png`)

    const axe = await new AxeBuilder({ page }).analyze()
    const found = axe.violations.flatMap((v) =>
      v.nodes.map((n) => `${v.id}: ${n.html} ${n.failureSummary ?? ''}`),
    )
    expect(found).toEqual([])
  })
}

test('day navigates between days', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/day')
  await expect(page.getByRole('button', { name: 'Nächster Tag' })).toBeDisabled()
  await page.getByRole('button', { name: 'Vorheriger Tag' }).click()
  await expect(page).toHaveURL(/date=2026-11-18/)
  await page.getByRole('button', { name: 'Heute' }).click()
  await expect(page).toHaveURL(/\/day$/)
  await expect(page.getByText(/von 96 Viertelstunden/)).toBeVisible()
})

test('day picks a date in the calendar and splits loadpoints', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/day?date=2026-11-18')
  await page.getByRole('button', { name: /^Tag wählen:/ }).click()
  const dialog = page.getByRole('dialog', { name: 'Tag wählen' })
  await expect(dialog.getByText('November 2026')).toBeVisible()
  await expect(dialog.getByRole('button', { name: /20\. November 2026/ })).toBeDisabled()
  await dialog.getByRole('button', { name: 'Dienstag, 10. November 2026' }).click()
  await expect(page).toHaveURL(/date=2026-11-10/)

  await page.getByRole('button', { name: 'Wallbox einzeln zeigen' }).click()
  await expect(page.getByRole('button', { name: 'wb-garage', exact: true })).toBeVisible()
  await expect(page.getByText('– wb-garage')).toBeVisible()
  await page.getByRole('button', { name: 'Wallbox zusammenfassen' }).click()
  await expect(page.getByRole('button', { name: 'wb-garage', exact: true })).toHaveCount(0)
})
