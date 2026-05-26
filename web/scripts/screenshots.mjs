// Capture screenshots of the live Ember stack for the docs site.
//
// Usage:
//   make embed build && docker compose -f deploy/docker-compose.yml up -d
//   (subscribe to a starter pack at https://localhost so there's something to see)
//   node web/scripts/screenshots.mjs
//
// Optional env:
//   EMBER_URL     default https://localhost
//   EMBER_USER    default admin
//   EMBER_PASS    default the value in deploy/.env's EMBER_ADMIN_PASSWORD
//   OUT_DIR       default docs/public/screenshots
//
// Output: PNGs land in OUT_DIR, ready for docs/screenshots.md to embed.
// Self-signed certs are accepted (this targets the homelab caddy with
// `tls internal`).

import { chromium } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const REPO_ROOT = path.resolve(path.dirname(__filename), "..", "..");

const URL = process.env.EMBER_URL || "https://localhost";
const USER = process.env.EMBER_USER || "admin";
const PASS = process.env.EMBER_PASS || readPasswordFromEnv() || "test";
const OUT_DIR =
  process.env.OUT_DIR || path.join(REPO_ROOT, "docs", "public", "screenshots");

function readPasswordFromEnv() {
  // Pull EMBER_ADMIN_PASSWORD out of deploy/.env if it exists. Saves the
  // operator from typing it in.
  const envPath = path.join(REPO_ROOT, "deploy", ".env");
  if (!fs.existsSync(envPath)) return null;
  const txt = fs.readFileSync(envPath, "utf8");
  const m = txt.match(/^EMBER_ADMIN_PASSWORD=(.+)$/m);
  return m ? m[1].trim() : null;
}

async function main() {
  fs.mkdirSync(OUT_DIR, { recursive: true });
  const browser = await chromium.launch();
  // Desktop shots
  const desktop = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    ignoreHTTPSErrors: true,
    deviceScaleFactor: 2,
  });
  await loginAndCapture(desktop, "desktop");
  await desktop.close();

  // Mobile shots
  const mobile = await browser.newContext({
    viewport: { width: 390, height: 844 },
    ignoreHTTPSErrors: true,
    deviceScaleFactor: 3,
    isMobile: true,
    hasTouch: true,
  });
  await loginAndCapture(mobile, "mobile");
  await mobile.close();

  await browser.close();
  console.log(`Wrote screenshots to ${OUT_DIR}`);
}

async function loginAndCapture(ctx, suffix) {
  const page = await ctx.newPage();
  // Login page
  await page.goto(URL, { waitUntil: "networkidle" });
  await page.fill('input[autocomplete="username"]', USER);
  await page.fill('input[autocomplete="current-password"]', PASS);
  await screenshot(page, `login-${suffix}`);
  await page.click('[data-testid="login-submit"]');

  // Wait for the shell to render.
  await page.waitForSelector("[data-testid=article-list]", { timeout: 30000 });

  // Reader shell (article list selected)
  await page.waitForTimeout(800);
  await screenshot(page, `reader-${suffix}`);

  // Open the first article
  const firstStory = page.locator("[data-testid^=story-]").first();
  if (await firstStory.count()) {
    await firstStory.click();
    await page.waitForTimeout(800);
    await screenshot(page, `article-${suffix}`);
  }

  // Settings → Preferences
  await page.locator("[data-user-chip]").click();
  await page.getByTestId("open-settings").click();
  await page.waitForSelector("[data-testid=settings]");
  await page.getByRole("button", { name: "Preferences" }).click();
  await page.waitForTimeout(400);
  await screenshot(page, `settings-preferences-${suffix}`);

  // Settings → Language model (admin only)
  const llmBtn = page.getByTestId("settings-llm");
  if (await llmBtn.count()) {
    await llmBtn.click();
    await page.waitForTimeout(400);
    await screenshot(page, `settings-llm-${suffix}`);
  }
}

async function screenshot(page, name) {
  const out = path.join(OUT_DIR, `${name}.png`);
  await page.screenshot({ path: out, fullPage: false });
  console.log(`  ${out}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
