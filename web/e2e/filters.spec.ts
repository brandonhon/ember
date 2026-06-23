import { test, expect, type Page } from "@playwright/test";
import { signIn } from "./helpers";

// openFilterManager opens Settings → Filters → "Open filter editor".
async function openFilterManager(page: Page): Promise<void> {
  await page.locator('[data-user-chip]').click();
  await page.getByTestId("open-settings").click();
  await expect(page.getByTestId("settings")).toBeVisible();
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  await page.getByTestId("open-filters").click();
  await expect(page.getByTestId("filter-manager")).toBeVisible();
}

test.describe("filters", () => {
  test("create, list, disable, and delete a filter via the UI", async ({ page }) => {
    await signIn(page);
    await openFilterManager(page);

    await page.getByTestId("filter-name").fill("hide espresso");
    await page.getByTestId("filter-field").selectOption("title");
    await page.getByTestId("filter-op").selectOption("contains");
    await page.getByTestId("filter-value").fill("espresso");
    await page.getByTestId("filter-action").selectOption("mark_read");
    await page.getByTestId("filter-save").click();

    const row = page.locator('[data-testid^="filter-row-"]', {
      hasText: "hide espresso",
    }).first();
    await expect(row).toBeVisible();

    await row.getByRole("button", { name: "Disable" }).click();
    await expect(row.getByRole("button", { name: "Enable" })).toBeVisible();

    await row.getByRole("button", { name: "Delete" }).click();
    await expect(row).toHaveCount(0);
  });

  test("import filters from a backup file", async ({ page }) => {
    await signIn(page);
    await openFilterManager(page);

    await page
      .getByTestId("filters-import-input")
      .setInputFiles("e2e/fixtures/filters.json");

    await expect(page.getByTestId("filters-notice")).toBeVisible();
    await expect(
      page
        .locator('[data-testid^="filter-row-"]', { hasText: "imported rule" })
        .first(),
    ).toBeVisible();
  });

  test("invalid filter shows an error and keeps the modal open", async ({ page }) => {
    await signIn(page);
    await openFilterManager(page);
    await page.getByTestId("filter-name").fill("incomplete");
    await page.getByTestId("filter-save").click();
    await expect(page.getByTestId("filter-error")).toBeVisible();
    await expect(page.getByTestId("filter-manager")).toBeVisible();
  });
});
