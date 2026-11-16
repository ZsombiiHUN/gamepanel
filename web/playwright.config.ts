import { defineConfig, devices } from "@playwright/test";

// Browser tests run against a real panel with a fake daemon (internal/e2e/testpanel),
// seeded with an admin, a regular user and 26 servers. Build the UI first (`pnpm build`),
// because the panel embeds it. `pnpm e2e` does both.
const PORT = Number(process.env.E2E_PORT ?? 4173);
const baseURL = `http://127.0.0.1:${PORT}`;
const sizes = /(layout|responsive|a11y)\.spec\.ts$/;

export default defineConfig({
  testDir: "e2e",
  outputDir: "e2e/.results",
  // The tests share one panel. Each creates its own throwaway servers instead of touching the seeded ones.
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [["github"], ["html", { open: "never", outputFolder: "e2e/.report" }]] : [["list"]],
  globalSetup: "./e2e/global-setup.ts",
  use: {
    baseURL,
    storageState: "e2e/.state/admin.json",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    // Set PW_CHROMIUM to use a browser Playwright did not download itself.
    launchOptions: process.env.PW_CHROMIUM ? { executablePath: process.env.PW_CHROMIUM } : {},
  },
  webServer: {
    command: `mkdir -p e2e/.state && go build -o ../bin/testpanel ../internal/e2e/testpanel && ../bin/testpanel -addr 127.0.0.1:${PORT} -ready e2e/.state/ready.json`,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: "ignore",
    stderr: "pipe",
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } } },
    { name: "laptop", testMatch: sizes, use: { ...devices["Desktop Chrome"], viewport: { width: 1366, height: 768 } } },
    { name: "mobile", testMatch: sizes, use: { ...devices["Pixel 7"], viewport: { width: 390, height: 844 } } },
    // The theme follows the system: browsers default to light here, so check the dark theme's contrast too.
    { name: "desktop-dark", testMatch: /a11y\.spec\.ts$/, use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 }, colorScheme: "dark" } },
  ],
});
