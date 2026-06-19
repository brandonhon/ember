import { test, expect, type Page } from "@playwright/test";
import { signIn } from "./helpers";

// z- prefix: runs LAST in the serial, shared-DB suite. These tests mark whole
// views read (emptying Fresh / All Unread), so they must not precede any spec
// that needs unread content. beforeEach resets every article back to UNREAD via
// the API, so each test starts from a full set regardless of suite order — the
// only way to test Today (keep) and All Unread (empty) on one shared DB, since
// they target the same unread set.
test.describe("mark all read per view", () => {
  // Mark every (non-muted, deduped) article unread so the view under test has a
  // full unread set. Returns how many were reset. Uses the app's own cookie +
  // CSRF header from the authenticated page context.
  async function resetAllUnread(page: Page): Promise<number> {
    return page.evaluate(async () => {
      const m = document.cookie.match(/(?:^|;\s*)ember_csrf=([^;]+)/);
      const csrf = m ? decodeURIComponent(m[1]) : "";
      const res = await fetch("/api/articles?limit=200&all=1", { credentials: "include" });
      const body = await res.json();
      const ids = (body.data ?? []).map((a: { id: number }) => a.id);
      if (ids.length) {
        await fetch("/api/articles/read", {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json", "X-Ember-CSRF": csrf },
          body: JSON.stringify({ ids, read: false }),
        });
      }
      return ids.length;
    });
  }

  test.beforeEach(async ({ page }) => {
    await signIn(page);
    const n = await resetAllUnread(page);
    expect(n).toBeGreaterThan(0);
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
  });

  const stories = (page: Page) => page.locator('[data-testid^="story-"]');

  test("Fresh: marking all read empties the list and reloads", async ({ page }) => {
    await page.getByTestId("view-fresh").click();
    await expect(stories(page).first()).toBeVisible();

    await page.getByTestId("mark-all-read").click();

    // Fresh lists only unread, so the read cards drop out (nothing left to page
    // in once the whole set is read).
    await expect(stories(page)).toHaveCount(0);
  });

  test("All Unread: marking all read empties the list AND clears the badge", async ({ page }) => {
    const au = page.locator(".nav-item", { hasText: "All Unread" });
    await au.click();
    await expect(stories(page).first()).toBeVisible();

    await page.getByTestId("mark-all-read").click();

    await expect(stories(page)).toHaveCount(0);
    // The badge must clear too — guards against the stale smart-count that left
    // "All Unread 53" hanging over an empty column until the next poll.
    await expect(au.locator(".badge")).toHaveCount(0);
  });

  test("Fresh: the article being read greys out on mark-all-read, then hides on the next click", async ({
    page,
  }) => {
    await page.getByTestId("view-fresh").click();
    await expect(stories(page).first()).toBeVisible();

    // Open the first article — selects it for the reader pane and auto-marks it
    // read. This is the card that should survive the first mark-all-read.
    const first = stories(page).first();
    const id = await first.getAttribute("data-article-id");
    await first.locator(".story-link").click();
    const opened = page.locator(`[data-testid="story-${id}"]`);
    await expect(opened).toBeVisible();

    // First mark-all-read: the open card is kept (greyed, data-is-read=1) so the
    // user can keep reading it, instead of dropping out like the rest of Fresh.
    await page.getByTestId("mark-all-read").click();
    await expect(opened).toBeVisible();
    await expect(opened).toHaveAttribute("data-is-read", "1");
    await expect(opened).toHaveClass(/read/);

    // Second mark-all-read: the one-shot grace is spent, so now it hides.
    await page.getByTestId("mark-all-read").click();
    await expect(opened).toHaveCount(0);
  });

  test("Today: marking all read KEEPS the (now-read) cards", async ({ page }) => {
    await page.locator(".nav-item", { hasText: "Today" }).click();
    await expect(stories(page).first()).toBeVisible();
    const before = await stories(page).count();

    await page.getByTestId("mark-all-read").click();

    // Today shows the calendar day's read + unread, so the cards stay put
    // (just flipped to read) rather than dropping out like Fresh/All Unread.
    await expect(stories(page)).toHaveCount(before);
  });
});
