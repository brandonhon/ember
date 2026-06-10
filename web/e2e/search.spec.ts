import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("search", () => {
  test("typeahead dropdown previews FTS matches", async ({ page }) => {
    await signIn(page);
    const search = page.getByTestId("search-input");
    await search.fill("espresso");
    const results = page.getByTestId("search-results");
    await expect(results).toBeVisible();
    await expect(results).toContainText("Second fixture about espresso");
  });

  test("submitting opens a dedicated search view in the article list", async ({ page }) => {
    await signIn(page);
    const search = page.getByTestId("search-input");
    await search.fill("espresso");
    await search.press("Enter");
    // Dropdown is dismissed; the article list now shows the search results.
    await expect(page.getByTestId("article-list")).toContainText("Search: espresso");
    await expect(page.getByTestId("article-list")).toContainText("Second fixture about espresso");
  });

  test("query with no matches shows no preview dropdown", async ({ page }) => {
    await signIn(page);
    const search = page.getByTestId("search-input");
    await search.fill("zzznothingmatchesthis");
    await expect(page.getByTestId("search-results")).toHaveCount(0);
  });

  test("arrow keys highlight a typeahead hit and Enter opens it in the reader", async ({ page }) => {
    await signIn(page);
    const search = page.getByTestId("search-input");
    await search.click();
    await search.fill("fixture");
    const results = page.getByTestId("search-results");
    await expect(results).toBeVisible();
    // Down-arrow highlights the first option.
    await search.press("ArrowDown");
    await expect(results.getByRole("option").first()).toHaveAttribute("aria-selected", "true");
    // Enter on the highlighted row opens that article (reader actions appear)
    // rather than submitting the full search.
    await search.press("Enter");
    await expect(page.getByTestId("search-results")).toBeHidden();
    await expect(page.getByTestId("reader-star")).toBeVisible();
  });
});
