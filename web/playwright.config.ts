import { defineConfig, devices } from "@playwright/test";
import { panelURL, PORT, WORKERS } from "./e2e/slots";

// Browser tests run against real panels with a fake daemon (internal/e2e/testpanel),
// each seeded with an admin, a regular user and 26 servers: one panel per worker
// (see e2e/slots.ts). Build the UI first (`pnpm build`), because the panel embeds it.
// `pnpm e2e` does both. E2E_WORKERS sets how many run at once.
const sizes = /(layout|responsive|a11y)\.spec\.ts$/;

export default defineConfig({
  testDir: "e2e",
  outputDir: "e2e/.results",
  // Tests in one worker share its panel, one at a time. Each creates its own throwaway servers instead of touching the seeded ones.
  fullyParallel: true,
  workers: WORKERS,
  retries: process.env.CI ? 1 : 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [["github"], ["html", { open: "never", outputFolder: "e2e/.report" }]] : [["list"]],
  globalSetup: "./e2e/global-setup.ts",
  // baseURL and storageState depend on the worker's panel, so e2e/support.ts sets them.
  use: {
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    // Set PW_CHROMIUM to use a browser Playwright did not download itself.
    launchOptions: process.env.PW_CHROMIUM ? { executablePath: process.env.PW_CHROMIUM } : {},
  },
  // Playwright starts these in order: the first builds the panel, the others reuse the binary.
  webServer: Array.from({ length: WORKERS }, (_, slot) => ({
    command: `${slot === 0 ? "go build -o ../bin/testpanel ../internal/e2e/testpanel && " : ""}mkdir -p e2e/.state/${slot} && ../bin/testpanel -addr 127.0.0.1:${PORT + slot} -ready e2e/.state/${slot}/ready.json`,
    url: panelURL(slot),
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: "ignore" as const,
    stderr: "pipe" as const,
  })),
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } } },
    { name: "laptop", testMatch: sizes, use: { ...devices["Desktop Chrome"], viewport: { width: 1366, height: 768 } } },
    { name: "mobile", testMatch: sizes, use: { ...devices["Pixel 7"], viewport: { width: 390, height: 844 } } },
    // The theme follows the system: browsers default to light here, so check the dark theme's contrast too.
    {
      name: "desktop-dark",
      testMatch: /a11y\.spec\.ts$/,
      use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 }, colorScheme: "dark" },
    },
  ],
});
