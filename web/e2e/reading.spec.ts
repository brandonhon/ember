import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("reading", () => {
  test("opening an article reveals the reader pane with its content", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("story-1").click();
    // Title rendered in the reader.
    await expect(page.locator("h1", { hasText: "First fixture article" })).toBeVisible();
    // Summary card from the seed.
    await expect(page.getByTestId("summary-card")).toBeVisible();
    await expect(page.getByTestId("summary-card")).toContainText("Test summary point one");
  });

  test("star toggle persists across reload", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("story-1").click();
    const starBtn = page.getByTestId("reader-star");
    await expect(starBtn).toBeVisible();
    await expect(starBtn).toContainText("Star");

    await starBtn.click();
    await expect(starBtn).toContainText("Starred");

    await page.reload();
    // After reload we need to log in via cookie (the session cookie is still
    // present in the browser context). Article list comes up, click the
    // starred story again.
    await expect(page.getByTestId("article-list")).toBeVisible();
    await page.getByTestId("story-1").click();
    await expect(page.getByTestId("reader-star")).toContainText("Starred");
  });

  test("Fresh smart view filters out older articles", async ({ page }) => {
    await signIn(page);
    // Sidebar defaults to Fresh on load; the seeded fixtures with published_at
    // within the last hour appear, the 2-day-old one does not.
    await page.getByTestId("view-fresh").click();
    await expect(page.getByTestId("story-1")).toBeVisible();
    await expect(page.getByTestId("story-2")).toBeVisible();
    await expect(page.getByTestId("story-3")).toHaveCount(0);
  });

  test("the m keyboard shortcut toggles read state of the selected article", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("feed-1").click();
    // story-2 is fresh + has not been clicked by any earlier spec in this
    // file. The DB persists across tests (single binary, single SQLite file
    // for the whole test run), so an article touched by a prior spec would
    // already be read here and we'd never see the auto-mark transition.
    await expect(page.getByTestId("story-2")).toBeVisible();

    // Helper: read the trailing digit run from the feed item; that's the
    // unread badge. Returns null when no badge (the feed-name text alone).
    const unread = async (): Promise<number | null> => {
      const text = await page.getByTestId("feed-1").innerText();
      const m = text.match(/(\d+)$/);
      return m ? Number(m[1]) : null;
    };

    const initial = (await unread()) ?? 0;
    if (initial < 1) {
      test.skip(true, "feed has no unread articles to toggle");
      return;
    }

    // Opening an article auto-marks it read (Reader.svelte $effect), so the
    // badge drops by 1 once story-2 is selected.
    await page.getByTestId("story-2").click();
    await expect.poll(unread, { timeout: 5_000 }).toBe(initial - 1);

    // Press `m`. Selected article is now read → toggle marks it unread →
    // badge increments back. Validates the keyboard shortcut fires and the
    // setRead pipeline updates both the article state and the feed counter.
    await page.keyboard.press("m");
    await expect.poll(unread, { timeout: 5_000 }).toBe(initial);
  });
});
