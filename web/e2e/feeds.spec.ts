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

  test("adding a feed URL via the top bar adds a row to the sidebar", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("add-feed-input").fill("https://added.test/feed");
    await page.getByTestId("add-feed-submit").click();
    // Wait for sidebar refresh.
    await expect(page.locator("button", { hasText: "added.test" })).toBeVisible({
      timeout: 5_000,
    });
  });

  test("clicking the seeded feed scopes the article list to that feed", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("feed-1").click();
    // The 3 seeded fixtures should each render as a story card.
    await expect(page.getByTestId("story-1")).toBeVisible();
    await expect(page.getByTestId("story-2")).toBeVisible();
    await expect(page.getByTestId("story-3")).toBeVisible();
  });
});
