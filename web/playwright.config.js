import { defineConfig, devices } from '@playwright/test'

// E2E runs against the real Go binary serving the embedded, production-built
// frontend, with gameplay phases shortened via FAUXTIST_FAST_PHASES_MS so a
// whole match runs in seconds. `npm run test:e2e` builds the frontend, embeds
// it, and compiles the binary first (see e2e/prep.mjs).
const PORT = 4599
const BIN = '/tmp/fauxlands-e2e-bin'

export default defineConfig({
  testDir: './e2e',
  timeout: 45_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: `FAUXTIST_FAST_PHASES_MS=1500 FAUXTIST_RECONNECT_GRACE_MS=4000 PORT=${PORT} ${BIN}`,
    url: `http://localhost:${PORT}/healthz`,
    reuseExistingServer: false,
    timeout: 20_000,
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    // Chromium at the spec's 390×844 portrait phone viewport. (WebKit-based
    // device presets like iPhone need a separate browser download; a Chromium
    // mobile emulation validates the same responsive layout.)
    {
      name: 'mobile',
      use: { browserName: 'chromium', viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 3 },
    },
  ],
})
