import { defineConfig, devices } from '@playwright/test';

const baseURL = 'http://127.0.0.1:4173';
const adminKey = 'playwright-admin-key';

export default defineConfig({
  testDir: './e2e',
  outputDir: 'test-results/playwright',
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'list',

  use: {
    baseURL,
    httpCredentials: {
      username: 'admin',
      password: adminKey,
    },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  webServer: {
    command: 'mkdir -p test-results && go build -o test-results/tinymarkdownnotes-e2e . && cd test-results && exec ./tinymarkdownnotes-e2e',
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
    gracefulShutdown: {
      signal: 'SIGTERM',
      timeout: 1_000,
    },
    env: {
      ADDR: '127.0.0.1:4173',
      NOTES_ADMIN_KEY: adminKey,
    },
  },
});
