import { defineConfig, devices } from '@playwright/test'

// UI visual and accessibility tests (spec 15): the built UI from `vite preview` against a
// mocked API (e2e/mock.ts), so no backend, network or real clock is involved.
const port = 4173

export default defineConfig({
  testDir: 'e2e',
  snapshotPathTemplate: '{testDir}/__screenshots__/{projectName}/{arg}{ext}',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  // Chromium rasterises glyphs at fractional positions slightly differently now and then
  // (same layout, ≈1 % of pixels); a fresh attempt renders it again. Real changes fail
  // every attempt.
  retries: 2,
  reporter: process.env.CI ? 'github' : 'list',
  expect: { toHaveScreenshot: { maxDiffPixelRatio: 0.002 } },
  use: {
    baseURL: `http://localhost:${port}`,
    locale: 'de-DE',
    timezoneId: 'Europe/Berlin',
    reducedMotion: 'reduce',
    // A controlling service worker would fetch past page.route; only pwa.spec allows it.
    serviceWorkers: 'block',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    // Coarse pointer and a narrow viewport select the phone layout (spec 11.2).
    { name: 'phone', use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    // A released-looking version, so the update check and the service worker act as in use.
    command: `VITE_APP_VERSION=1.0.0-e2e npx vite build && npx vite preview --port ${port} --strictPort`,
    port,
    reuseExistingServer: !process.env.CI,
  },
})
