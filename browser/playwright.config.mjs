import { defineConfig, devices } from "@playwright/test";
import os from "node:os";
import path from "node:path";

const baseURL = process.env.SYMMETRY_PORTAL_URL || "http://127.0.0.1:4000";
const outputDir =
  process.env.SYMMETRY_PLAYWRIGHT_OUTPUT || path.join(os.tmpdir(), "symmetry-playwright-results");

export default defineConfig({
  testDir: "./tests",
  outputDir,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  // A maximum wait, never a weaker observation: these suites share the host
  // with live daemons whose runs stream events, and one real conversation read
  // of a live run was measured at 9.9 s under that load.
  expect: { timeout: 30_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure"
  },
  projects: [
    {
      name: "desktop-chromium",
      use: { ...devices["Desktop Chrome"] }
    },
    {
      name: "mobile-chromium",
      use: { ...devices["Pixel 7"] }
    }
  ]
});
