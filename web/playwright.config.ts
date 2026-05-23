import { defineConfig, devices } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");
const dbPath = path.resolve(repoRoot, "web", ".playwright-data", "ember.db");

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false, // single binary + single SQLite → run serially
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: process.env.EMBER_E2E_URL ?? "http://localhost:8090",
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  webServer: process.env.EMBER_E2E_URL
    ? undefined
    : {
        // Build is the responsibility of CI / the developer running tests;
        // here we just launch the existing binary in test mode against a
        // fresh on-disk SQLite file.
        command: `rm -f ${dbPath} && ${path.resolve(repoRoot, "bin/ember")}`,
        url: "http://localhost:8090/healthz",
        reuseExistingServer: false,
        timeout: 30_000,
        env: {
          EMBER_TEST_MODE: "1",
          EMBER_ADDR: ":8090",
          EMBER_DB_PATH: dbPath,
          EMBER_LOG_LEVEL: "warn",
        },
      },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
