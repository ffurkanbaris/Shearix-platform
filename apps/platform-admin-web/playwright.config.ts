import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e", fullyParallel: true,
  expect: { timeout: 15_000 },
  webServer: { command: "npm run dev -- --hostname 0.0.0.0 --port 3100", url: "http://127.0.0.1:3100", reuseExistingServer: !process.env.CI },
  use: { baseURL: "http://platform.localhost:3100", trace: "retain-on-failure" },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } } },
    { name: "tablet", use: { ...devices["Desktop Chrome"], viewport: { width: 768, height: 1024 } } },
    { name: "mobile", use: { ...devices["Desktop Chrome"], viewport: { width: 360, height: 800 } } },
  ],
});
