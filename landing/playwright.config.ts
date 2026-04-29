import { defineConfig, devices } from "@playwright/test";

// Drives the full Connect stack end-to-end against the same services the
// developer uses locally: landing on :4322, cloud worker on :8787,
// relay worker on :8788. `reuseExistingServer` lets Playwright attach to a
// stack already booted via `task dev:hosted`; otherwise it starts each one.

const BASE_URL = "http://localhost:4322";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  use: {
    baseURL: BASE_URL,
    trace: "retain-on-failure",
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
  },
  webServer: [
    {
      command: "npm run dev -- --port 4322",
      cwd: ".",
      url: BASE_URL,
      reuseExistingServer: true,
      timeout: 60_000,
    },
    {
      command: "npm run dev",
      cwd: "../cloud",
      url: "http://localhost:8787/health",
      reuseExistingServer: true,
      timeout: 60_000,
    },
    {
      command: "npm run dev",
      cwd: "../relay",
      url: "http://localhost:8788/health",
      reuseExistingServer: true,
      timeout: 60_000,
    },
  ],
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
