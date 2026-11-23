import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import type { Me, SiteModel } from '../src/api/types'
import { shortPassword } from './admin'
import { mockServer } from './mock'

// Login, setup and settings (spec 11.3, 11.14), light and dark, and axe.
const site = JSON.parse(
  readFileSync(new URL('../../internal/app/testdata/site-r4.json', import.meta.url), 'utf8'),
) as SiteModel
const admin: Me = { user: { id: 1, name: 'admin', via: 'session' }, setup_required: false, public_read: true }
const closed: Me = { user: null, setup_required: false, public_read: false }
const fresh: Me = { user: null, setup_required: true, public_read: true }

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

for (const scheme of ['light', 'dark'] as const) {
  test(`settings ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site, { me: admin })
    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: 'Einstellungen', level: 1 })).toBeVisible()
    await expect(page.getByText('home-assistant')).toBeVisible()
    await expect(page.getByText('config_change')).toBeVisible()
    await shoot(page, `settings-${scheme}.png`)
  })

  test(`login ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme })
    await mockServer(page, site, { me: closed })
    await page.goto('/day')
    await expect(page).toHaveURL(/\/login\?next=\/day/)
    await expect(page.getByRole('heading', { name: 'Anmelden' })).toBeVisible()
    await shoot(page, `login-${scheme}.png`)
  })
}

test('setup', async ({ page }) => {
  await mockServer(page, site, { me: fresh })
  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await page.getByLabel('Setup-Code').fill('000000')
  await page.getByLabel('Name').fill('admin')
  await page.getByLabel('Passwort', { exact: true }).fill(shortPassword)
  await page.getByLabel('Passwort wiederholen').fill('other')
  await page.getByRole('button', { name: 'Konto anlegen' }).click()
  await expect(page.getByText('Die Passwörter stimmen nicht überein.')).toBeVisible()
  await page.getByLabel('Passwort wiederholen').fill(shortPassword)
  await page.getByRole('button', { name: 'Konto anlegen' }).click()
  await expect(page.getByText('Der Setup-Code ist falsch.')).toBeVisible()
  await page.getByLabel('Setup-Code').fill('123456')
  await page.getByRole('button', { name: 'Konto anlegen' }).click()
  await expect(page.getByText(/Das Passwort ist zu kurz/)).toBeVisible()
  await shoot(page, 'setup.png')
})

test('login returns to the page asked for', async ({ page }) => {
  let me: Me = closed
  await mockServer(page, site, { me: closed })
  await page.route('**/api/v1/auth/me', (r) =>
    r.fulfill({ contentType: 'application/json', body: JSON.stringify(me) }),
  )
  await page.goto('/history')
  await page.getByLabel('Name').fill('admin')
  await page.getByLabel('Passwort').fill('wrong')
  await page.getByRole('button', { name: 'Anmelden' }).click()
  await expect(page.getByText('Name oder Passwort falsch.')).toBeVisible()
  me = admin
  await page.getByLabel('Passwort').fill('correct horse battery')
  await page.getByRole('button', { name: 'Anmelden' }).click()
  await expect(page).toHaveURL(/\/history$/)
})

test('settings show field problems and the new token once', async ({ page }) => {
  await mockServer(page, site, { me: admin })
  await page.goto('/settings')
  await page.getByLabel('Intervall der Live-Berechnung').fill('soon')
  await page.getByRole('button', { name: 'Speichern' }).first().click()
  await expect(page.getByText('invalid duration "soon"')).toBeVisible()
  await expect(page.getByLabel('Öffentlicher Lesezugriff')).toBeDisabled()
  await page.getByLabel('Name', { exact: true }).last().fill('grafana')
  await page.getByRole('button', { name: 'Erstellen' }).click()
  await expect(page.getByText('pvm_n3w_s3cr3tvalue')).toBeVisible()
  const revoke = page.getByRole('button', { name: 'Widerrufen' })
  await revoke.click()
  await expect(page.getByRole('button', { name: 'Widerrufen – sicher?' })).toBeVisible()
})

test('admin pages need a login', async ({ page }) => {
  await mockServer(page, site)
  await page.goto('/settings')
  await expect(page).toHaveURL(/\/login\?next=\/settings$/)
  await expect(page.getByRole('link', { name: 'Zurück zur Übersicht' })).toBeVisible()
})
