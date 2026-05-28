// Render docs/public/social-preview.png from web/scripts/social-preview.html.
//
// The PNG is what GitHub serves as the OG card when the repo URL is shared
// on Twitter / Slack / Discord / Bluesky. Upload via the GitHub UI:
//   Settings -> General -> Social preview.
// (The file lives in docs/public so its source is reproducible; GitHub does
//  NOT auto-pick it up.)
//
// Usage:
//   node web/scripts/social-preview.mjs
//
// Optional env:
//   OUT     default docs/public/social-preview.png

import { chromium } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const HERE = path.dirname(__filename);
const REPO_ROOT = path.resolve(HERE, "..", "..");
const SRC = path.join(HERE, "social-preview.html");
const OUT =
  process.env.OUT || path.join(REPO_ROOT, "docs", "public", "social-preview.png");

const browser = await chromium.launch();
const ctx = await browser.newContext({
  viewport: { width: 1280, height: 640 },
  deviceScaleFactor: 1, // GitHub spec is exactly 1280x640, not @2x.
});
const page = await ctx.newPage();
await page.goto("file://" + SRC, { waitUntil: "networkidle" });
// Give web fonts a beat to settle even after networkidle.
await page.waitForTimeout(400);
await page.screenshot({ path: OUT, type: "png", omitBackground: false });
await browser.close();
console.log("wrote " + OUT);
