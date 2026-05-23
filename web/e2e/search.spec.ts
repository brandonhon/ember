import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("search", () => {
  test("FTS query returns the matching seeded article", async ({ page }) => {
    await signIn(page);
    const search = page.getByTestId("search-input");
    await search.fill("espresso");
    await search.press("Enter");
    const results = page.getByTestId("search-results");
    await expect(results).toBeVisible();
    await expect(results).toContainText("Second fixture about espresso");
  });

  test("query with no matches shows empty results", async ({ page }) => {
    await signIn(page);
    const search = page.getByTestId("search-input");
    await search.fill("zzznothingmatchesthis");
    await search.press("Enter");
    // No results panel = either hidden or empty.
    await expect(page.getByTestId("search-results")).toHaveCount(0);
  });
});
