import { test, expect, type Page } from "@playwright/test";
import { signIn } from "./helpers";

// The real server only emits the update hint on a clean tagged build once a
// newer GitHub release exists — never in test mode. So we intercept /api/me and
// graft an `update` object onto the genuine response. Using route.fetch() +
// fulfill({ response, json }) keeps every other field intact, including the
// sliding-session cookie the server re-sets on each request.
const UPDATE = {
  current: "v0.9.4",
  latest: "v0.9.5",
  available: true,
  url: "https://github.com/brandonhon/ember/releases/tag/v0.9.5",
  published: "2026-07-01T00:00:00Z",
  checked_at: "2026-07-09T00:00:00Z",
};

async function injectUpdate(page: Page, update: unknown = UPDATE): Promise<void> {
  await page.route("**/api/me", async (route) => {
    const resp = await route.fetch();
    // The boot (pre-login) /api/me is rejected by the auth middleware as
    // plain-text "unauthorized" — pass anything that isn't a 200 JSON body
    // through untouched so we only graft the hint onto the real payload.
    const ct = resp.headers()["content-type"] ?? "";
    if (resp.status() !== 200 || !ct.includes("application/json")) {
      await route.fulfill({ response: resp });
      return;
    }
    const body = await resp.json();
    if (body?.data) body.data.update = update;
    await route.fulfill({ response: resp, json: body });
  });
}

test.describe("update banner", () => {
  test("shows for an admin, links to the release, and dismissal persists", async ({ page }) => {
    await injectUpdate(page);
    await signIn(page);

    const banner = page.getByTestId("update-banner");
    await expect(banner).toBeVisible();
    await expect(banner).toContainText("Ember v0.9.5 is available");
    await expect(page.getByTestId("update-banner-link")).toHaveAttribute("href", UPDATE.url);

    // Visual artifact (lands in the gitignored test-results dir).
    await page.screenshot({ path: test.info().outputPath("update-banner.png") });

    // Dismiss, and it stays gone across a reload — per-version localStorage.
    await page.getByTestId("update-banner-dismiss").click();
    await expect(banner).toBeHidden();
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect(page.getByTestId("update-banner")).toHaveCount(0);
  });

  test("About section shows the update hint with a release-notes link", async ({ page }) => {
    await injectUpdate(page);
    await signIn(page);

    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByTestId("settings-about").click();

    await expect(page.getByTestId("about-update-link")).toHaveAttribute("href", UPDATE.url);
  });

  test("no banner when the server reports no update available", async ({ page }) => {
    await injectUpdate(page, { ...UPDATE, available: false, latest: "v0.9.4" });
    await signIn(page);

    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect(page.getByTestId("update-banner")).toHaveCount(0);
  });
});
