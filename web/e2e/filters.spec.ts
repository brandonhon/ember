import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("filters", () => {
  test("create, list, disable, and delete a filter via the UI", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("open-filters").click();
    await expect(page.getByTestId("filter-manager")).toBeVisible();

    // Fill the form.
    await page.getByTestId("filter-name").fill("hide espresso");
    await page.getByTestId("filter-field").selectOption("title");
    await page.getByTestId("filter-op").selectOption("contains");
    await page.getByTestId("filter-value").fill("espresso");
    await page.getByTestId("filter-action").selectOption("mark_read");
    await page.getByTestId("filter-save").click();

    // The new row appears in the modal list.
    const row = page.locator('[data-testid^="filter-row-"]', {
      hasText: "hide espresso",
    }).first();
    await expect(row).toBeVisible();

    // Disable it via the row action.
    await row.getByRole("button", { name: "Disable" }).click();
    await expect(row.getByRole("button", { name: "Enable" })).toBeVisible();

    // Delete it.
    await row.getByRole("button", { name: "Delete" }).click();
    await expect(row).toHaveCount(0);
  });

  test("invalid filter shows an error and keeps the modal open", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("open-filters").click();
    // Skip the value field so client-side validation rejects.
    await page.getByTestId("filter-name").fill("incomplete");
    await page.getByTestId("filter-save").click();
    await expect(page.getByTestId("filter-error")).toBeVisible();
    await expect(page.getByTestId("filter-manager")).toBeVisible();
  });
});
