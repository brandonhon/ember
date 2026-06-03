// One-shot capture of a light-theme three-pane reader screenshot for the
// docs hero, matched dimension-for-dimension to the existing dark hero
// (2880×1800 retina). Assumes a test-mode `./bin/ember` is running on
// :8080. Run from repo root:
//
//   make embed build
//   EMBER_TEST_MODE=1 ./bin/ember &
//   sleep 1
//   node pics/capture-hero-light.mjs
//   kill %1
//
// Output: pics/hero-2-threepane-summary-light.png

// Playwright lives in web/node_modules; this script sits in pics/. Node
// resolves bare imports from the script's directory, so the explicit
// relative path keeps the script runnable from anywhere.
import { chromium } from "../web/node_modules/@playwright/test/index.mjs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const URL = process.env.EMBER_URL || "http://localhost:8080";
const OUT = path.join(REPO_ROOT, "pics", "hero-2-threepane-summary-light.png");

const browser = await chromium.launch();
const ctx = await browser.newContext({
  viewport: { width: 1440, height: 900 },
  deviceScaleFactor: 2,
  ignoreHTTPSErrors: true,
});
const page = await ctx.newPage();

// Pin light theme + suppress the first-run welcome modal BEFORE the app
// boots so we never flash the dark default or the welcome card.
await page.addInitScript(() => {
  try {
    localStorage.setItem("ember:theme", "light");
    localStorage.setItem("ember:welcome-seen", "1");
  } catch {}
});

await page.goto(URL, { waitUntil: "networkidle" });

// Login with the test-mode seeded admin (cmd/ember/seed.go).
await page.fill('input[autocomplete="username"]', "admin");
await page.fill('input[autocomplete="current-password"]', "admintest");
await page.click('[data-testid="login-submit"]');
await page.waitForSelector("[data-testid=article-list]", { timeout: 30000 });

// Pick a story that has a summary (test fixtures stamp summary_model on
// most rows). Click the first one and wait for the reader to populate.
const firstStory = page.locator("[data-testid^=story-]").first();
await firstStory.click();
await page.waitForSelector(".article-body", { timeout: 10000 });

// Settle for layout + summary card fade-in.
await page.waitForTimeout(800);

await page.screenshot({ path: OUT, fullPage: false });
console.log(`Wrote ${OUT}`);

await browser.close();
