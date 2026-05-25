import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("saved searches", () => {
  test("create a saved search, run it, then delete it", async ({ page }) => {
    await signIn(page);
    // Open the inline form
    await page.getByTestId("open-add-search").click();
    await page.getByTestId("add-search-name").fill("Espresso watch");
    await page.getByTestId("add-search-query").fill("espresso");
    await page.getByTestId("add-search-submit").click();

    // Sidebar now has the saved search entry. Locate the whole row by
    // looking for an inner span with the given label.
    const row = page.locator(".feed-row", { hasText: "Espresso watch" });
    await expect(row).toBeVisible();

    // Click it -> article list switches to the search view + shows matches.
    await row.locator(".nav-item").click();
    await expect(page.getByTestId("article-list")).toContainText("Search: espresso");

    // Delete via the row's × button (opacity:0 until hover, but clickable).
    await row.hover();
    await row.locator(".board-delete").click({ force: true });
    await page.getByTestId("confirm-ok").click();
    await expect(row).toHaveCount(0);
  });
});
