// Dual-theme screenshot capture for the docs site.
//
// Runs the test-mode `./bin/ember` binary (no Docker needed) and captures
// each "scene" twice — once in light theme, once in dark — at desktop and
// mobile viewports. Output filenames are
//   docs/public/screenshots/<scene>-<viewport>-<theme>.png
// e.g. reader-desktop-light.png / reader-desktop-dark.png.
//
// Desktop settings scenes are captured at full pane height: the modal's 640px
// cap is lifted at capture time and the viewport grown to fit, so the entire
// settings pane lands in one image instead of just its first card.
//
// The companion `.light-only` / `.dark-only` CSS classes in
// docs/.vitepress/theme/style.css hide whichever variant doesn't match
// the current docs site theme, so the screenshot always contrasts with
// the page background.
//
// Usage from repo root:
//   make embed build
//   EMBER_TEST_MODE=1 EMBER_ADDR=:8083 EMBER_DB_PATH=/tmp/ember-shots.db ./bin/ember &
//   node web/scripts/screenshots-dual-theme.mjs
//   kill %1

import { chromium } from "../node_modules/@playwright/test/index.mjs";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const URL = process.env.EMBER_URL || "http://localhost:8083";
const USER = process.env.EMBER_USER || "admin";
const PASS = process.env.EMBER_PASS || "admintest";
const OUT_DIR = process.env.OUT_DIR || path.join(REPO_ROOT, "docs", "public", "screenshots");

const VIEWPORTS = [
  { name: "desktop", width: 1440, height: 900, mobile: false },
  { name: "mobile",  width: 390,  height: 844, mobile: true  },
];
const THEMES = ["light", "dark"];

// `.backdrop` pads the modal by 24px on every side.
const BACKDROP_PAD = 24;

// The settings modal is deliberately height-capped in the app
// (`.modal { height: min(640px, 86vh) }`) with an internally scrolling
// `.content`, so a viewport-sized shot catches only the first card or two --
// the Language model pane alone is ~2450px of content. For the docs we want the
// whole pane in one frame, so let the modal size to its content and grow the
// viewport to match. Every pixel is still real app rendering, app chrome
// included; only the scroll container is unclamped.
const EXPAND_MODAL_CSS = `
  [data-testid=settings] { place-items: start center !important; }
  [data-testid=settings] .modal { height: auto !important; max-height: none !important; overflow: visible !important; }
  [data-testid=settings] .layout { overflow: visible !important; }
  [data-testid=settings] .content { overflow: visible !important; }
`;

fs.mkdirSync(OUT_DIR, { recursive: true });

const browser = await chromium.launch();
try {
  for (const vp of VIEWPORTS) {
    for (const theme of THEMES) {
      console.log(`==> ${vp.name} / ${theme}`);
      const ctx = await browser.newContext({
        viewport: { width: vp.width, height: vp.height },
        deviceScaleFactor: 2,
        ignoreHTTPSErrors: true,
        isMobile: vp.mobile,
        hasTouch: vp.mobile,
      });
      const page = await ctx.newPage();

      // Set theme + suppress the first-run welcome modal before the app
      // boots so we never flash defaults during capture.
      await page.addInitScript((t) => {
        try {
          localStorage.setItem("ember:theme", t);
          localStorage.setItem("ember:welcome-seen", "1");
        } catch {}
      }, theme);

      await captureScenes(page, vp, theme);
      await ctx.close();
    }
  }
} finally {
  await browser.close();
}
console.log(`Wrote screenshots to ${OUT_DIR}`);

async function captureScenes(page, vp, theme) {
  // 1. Login page — already on the landing route before auth.
  await page.goto(URL, { waitUntil: "networkidle" });
  await shoot(page, "login", vp, theme);

  // Sign in.
  await page.fill('input[autocomplete="username"]', USER);
  await page.fill('input[autocomplete="current-password"]', PASS);
  await page.click('[data-testid="login-submit"]');
  await page.waitForSelector("[data-testid=article-list]", { timeout: 30000 });

  // Dismiss the welcome modal if it sneaks in.
  const welcome = page.locator('[data-testid="welcome-modal"]');
  if (await welcome.count()) {
    await page.keyboard.press("Escape").catch(() => {});
    await welcome.waitFor({ state: "hidden", timeout: 4000 }).catch(() => {});
  }

  // 2. Three-pane reader shell (article list visible).
  await page.waitForTimeout(700);
  await shoot(page, "reader", vp, theme);

  // 3. Open the first article → article view with summary card.
  const first = page.locator("[data-testid^=story-]").first();
  if (await first.count()) {
    await first.click();
    await page.waitForSelector(".article-body", { timeout: 8000 }).catch(() => {});
    await page.waitForTimeout(800);
    await shoot(page, "article", vp, theme);
  }

  // 4-6. Settings panes. Each pane is best-effort: a missing button or
  // visibility hiccup logs + skips rather than aborting the whole run.
  await page.locator("[data-user-chip]").click();
  await page.getByTestId("open-settings").click();
  await page.waitForSelector("[data-testid=settings]");

  // Mobile settings is a full-screen takeover with its own drill-down layout,
  // so the unclamping only applies to desktop.
  if (!vp.mobile) await page.addStyleTag({ content: EXPAND_MODAL_CSS });

  await capturePane(page, "Preferences",     "settings-preferences", vp, theme);
  await capturePane(page, "Language model",  "settings-llm",         vp, theme);
  await capturePane(page, "Email / SMTP",    "settings-email",       vp, theme);
}

async function capturePane(page, sceneTitle, testId, vp, theme) {
  try {
    // Resolve by data-testid OR by visible button text — covers both the
    // admin-gated panes (which use testids) and the always-on ones.
    let btn = page.getByTestId(testId);
    if (!(await btn.count())) {
      btn = page.getByRole("button", { name: sceneTitle });
    }
    if (!(await btn.count())) {
      console.log(`   (skipped: ${testId} — pane not present, likely non-admin user)`);
      return;
    }
    // Scroll into view first; the nav inside the settings modal can be
    // a scroll container at small heights and Playwright won't auto-scroll
    // into modal overlays reliably.
    await btn.scrollIntoViewIfNeeded({ timeout: 4000 }).catch(() => {});
    await btn.click({ timeout: 4000 });
    await page.waitForTimeout(400);
    if (!vp.mobile) await fitViewportToModal(page, vp);
    await shoot(page, testId, vp, theme);
    // Restore, so the next pane is measured from a known viewport.
    if (!vp.mobile) await page.setViewportSize({ width: vp.width, height: vp.height });
  } catch (err) {
    console.log(`   (skipped: ${testId} — ${err.message.split("\n")[0]})`);
  }
}

// Grow the viewport so the content-sized modal fits without clipping. The modal
// height is driven by its content (width is fixed at 880px), so it does not
// depend on the viewport height we are setting here.
async function fitViewportToModal(page, vp) {
  const h = await page.evaluate(() => {
    const m = document.querySelector("[data-testid=settings] .modal");
    return m ? Math.ceil(m.getBoundingClientRect().height) : 0;
  });
  if (!h) return;
  await page.setViewportSize({
    width: vp.width,
    height: Math.max(vp.height, h + BACKDROP_PAD * 2),
  });
  await page.waitForTimeout(300);
}

async function shoot(page, scene, vp, theme) {
  const file = path.join(OUT_DIR, `${scene}-${vp.name}-${theme}.png`);
  await page.screenshot({ path: file, fullPage: false });
  console.log(`   ${path.relative(REPO_ROOT, file)}`);
}
