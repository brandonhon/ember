import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("feeds", () => {
  test("seeded feed appears in sidebar with unread badge", async ({ page }) => {
    await signIn(page);
    const feedRow = page.getByTestId("feed-1");
    await expect(feedRow).toBeVisible();
    await expect(feedRow).toContainText("Example Tech Blog");
    // Seeded fixtures, none read yet — count > 0.
    const text = await feedRow.innerText();
    const n = Number(text.match(/(\d+)$/)?.[1] ?? 0);
    expect(n).toBeGreaterThan(0);
  });

  test("adding a feed URL via the sidebar adds a row", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("open-add-feed").click();
    await page.getByTestId("add-feed-input").fill("https://added.test/feed");
    await page.getByTestId("add-feed-submit").click();
    await expect(page.locator("button", { hasText: "added.test" })).toBeVisible({
      timeout: 5_000,
    });
  });

  test("clicking the seeded feed scopes the article list to that feed", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("feed-1").click();
    // The feed view is bounded by the reading window (24h), so the two recent
    // fixtures render and the 2-day-old one (story-3) is hidden — the same
    // window the Fresh view and the feed's unread badge use.
    await expect(page.getByTestId("story-1")).toBeVisible();
    await expect(page.getByTestId("story-2")).toBeVisible();
    await expect(page.getByTestId("story-3")).toHaveCount(0);
  });
});
