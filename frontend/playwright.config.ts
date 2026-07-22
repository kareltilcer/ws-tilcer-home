import { defineConfig } from '@playwright/test'

// Two managed servers: the Go backend (dev auth bypass) and the Vite dev server
// (which proxies /api + /ws to the backend). Paths/env come from the runner.
const SCRATCH =
  'C:/Users/karel/AppData/Local/Temp/claude/C--Users-karel-WebstormProjects-ws-tilcer-home/b8c23a91-47fe-4cf7-ac41-f1118d04f360/scratchpad'
const BACKEND_EXE = process.env.BACKEND_EXE ?? `${SCRATCH}/home-e2e.exe`
const E2E_DB = process.env.E2E_DB ?? `${SCRATCH}/e2e-playwright.db`

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: 'http://localhost:5199',
    trace: 'off',
  },
  webServer: [
    {
      command: `"${BACKEND_EXE}"`,
      url: 'http://127.0.0.1:8080/healthz',
      timeout: 60_000,
      reuseExistingServer: false,
      env: {
        HOME_DB_PATH: E2E_DB,
        HOME_DEV_AUTH_BYPASS: 'true',
        HOME_ADDR: '127.0.0.1:8080',
      },
    },
    {
      command: 'npm run dev -- --port 5199 --strictPort',
      url: 'http://localhost:5199',
      timeout: 120_000,
      reuseExistingServer: false,
    },
  ],
})
