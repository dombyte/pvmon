import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import type { Me, SiteModel } from '../src/api/types'
import { mockServer } from './mock'

// The setup wizard (spec 11.14 steps 2–6), light and dark, and axe.
const site = JSON.parse(
  readFileSync(new URL('../../internal/app/testdata/site-r4.json', import.meta.url), 'utf8'),
) as SiteModel
const admin: Me = { user: { id: 1, name: 'admin', via: 'session' }, setup_required: false, public_read: true }

async function shoot(page: Page, name: string): Promise<void> {
  await page.evaluate(() => document.fonts.ready)
  const size = page.viewportSize() ?? { width: 0, height: 0 }
  const overflow = await page
    .locator('main')
    .first()
    .evaluate((m) => m.scrollHeight - m.clientHeight)
  await page.setViewportSize({ width: size.width, height: size.height + Math.max(0, overflow) })
  await expect(page).toHaveScreenshot(name)
  const axe = await new AxeBuilder({ page }).analyze()
  const found = axe.violations.flatMap((v) =>
    v.nodes.map((n) => `${v.id}: ${n.html} ${n.failureSummary ?? ''}`),
  )
  expect(found).toEqual([])
}

async function choose(page: Page, label: string, option: RegExp | string): Promise<void> {
  await page.getByRole('button', { name: new RegExp(`^${label}:`) }).click()
  await page.getByRole('option', { name: option }).click()
}

/** Adds the inverter after a successful test read. */
async function addInverter(page: Page, host = '10.0.0.20'): Promise<void> {
  await page.getByRole('button', { name: 'Gerät hinzufügen' }).click()
  await choose(page, 'Profil', /solis\/s6-hybrid/)
  await page.getByLabel('Host oder IP-Adresse').fill(host)
  await page.getByRole('button', { name: 'Testen' }).click()
}

for (const scheme of ['light', 'dark'] as const) {
  test(`wizard devices ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site, { me: admin })
    await page.goto('/wizard')
    await addInverter(page)
    await expect(page.getByText('Gelesen in 84 ms.')).toBeVisible()
    await expect(page.getByText('3,2 kW').first()).toBeVisible()
    await shoot(page, `wizard-devices-${scheme}.png`)
  })
}

test('wizard meters, hierarchy and check', async ({ page }) => {
  await mockServer(page, site, { me: admin })
  await page.goto('/wizard')
  await addInverter(page)
  await page.getByRole('button', { name: 'Gerät hinzufügen' }).first().click()
  await expect(page.getByText('s6-hybrid', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Weiter' }).click()
  await expect(page.getByRole('heading', { name: 'Zähler' })).toBeVisible()
  // The shelly's grid meter stays the grid; the inverter's grid channel is diagnostic.
  const inverter = page.getByRole('region', { name: 's6-hybrid' })
  await expect(inverter.getByRole('button', { name: 'Rolle: Wechselrichter' })).toBeVisible()
  await expect(inverter.getByRole('checkbox', { name: /pv2/ })).not.toBeChecked()
  await expect(inverter.getByRole('checkbox', { name: /pv1/ })).toBeChecked()
  await shoot(page, 'wizard-meters.png')

  await page.getByRole('button', { name: 'Weiter' }).click()
  await page.getByLabel('Kapazität (kWh)').fill('10')
  await expect(page.getByRole('button', { name: 'Enthalten in: pv' })).toBeVisible()
  await shoot(page, 'wizard-hierarchy.png')

  await page.getByRole('button', { name: 'Weiter' }).click()
  await expect(page.getByText('Die Bilanz ist plausibel.')).toBeVisible()
  await expect(page.getByText(/= Verbrauch 1,2\skW/)).toBeVisible()
  await shoot(page, 'wizard-check.png')

  await page.getByRole('button', { name: 'Weiter' }).click()
  await expect(page.getByText('pv-pv1')).toBeVisible()
  await page.getByRole('button', { name: 'Konfiguration speichern' }).click()
  await expect(page.getByText('Gespeichert und übernommen.')).toBeVisible()
})

test('wizard reports a silent device and a rejected save', async ({ page }) => {
  await mockServer(page, site, { me: admin })
  await page.goto('/wizard')
  await addInverter(page, '10.0.0.99')
  await expect(page.getByText(/Das Gerät hat nicht geantwortet: .*i\/o timeout/)).toBeVisible()

  await page.getByRole('button', { name: '3. Hierarchie' }).click()
  await page.getByRole('button', { name: '2. Zähler' }).click()
  await page.getByRole('region', { name: 'shelly' }).getByRole('checkbox', { name: /grid/ }).uncheck()
  await page.getByRole('button', { name: '5. Speichern' }).click()
  await page.getByRole('button', { name: 'Konfiguration speichern' }).click()
  await expect(page.getByText('exactly one grid meter is required, found none')).toBeVisible()
})

test('an empty site leads to the wizard', async ({ page }) => {
  await mockServer(page, { ...site, meters: [], devices: [] }, { me: admin })
  await page.goto('/')
  await page.getByRole('link', { name: 'Einrichtungsassistent starten' }).click()
  await expect(page).toHaveURL(/\/wizard/)
})
