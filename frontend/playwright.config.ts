import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './tests',
  testMatch: '*.spec.ts',
  workers: 1,
  use: {
    baseURL: 'http://127.0.0.1:4178',
    viewport: { width: 1440, height: 1000 },
    launchOptions: { executablePath: process.env.CHROMIUM_PATH || undefined },
  },
  webServer: {
    command: 'node tests/fixture-server.mjs',
    url: 'http://127.0.0.1:4178',
    reuseExistingServer: !process.env.CI,
  },
  reporter: 'list',
})
