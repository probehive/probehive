import { defineConfig, devices } from '@playwright/test'

// End-to-end journeys against the real API and a dedicated disposable PostgreSQL
// database. See docs/development.md; requires the development database server of
// deploy/compose/compose.dev.yaml (or PROBEHIVE_E2E_PG* overrides) plus browsers
// from `npx playwright install chromium`.
export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  // The first-run journey mutates global instance state (setup), so one worker.
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: 'bash e2e/start-api.sh',
      url: 'http://127.0.0.1:5080/readyz',
      // Never reuse a running API: the journey depends on a freshly reset database.
      reuseExistingServer: false,
      timeout: 180_000,
    },
    {
      command: 'npm run dev -- --host 127.0.0.1 --port 5173 --strictPort',
      url: 'http://127.0.0.1:5173',
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
})
