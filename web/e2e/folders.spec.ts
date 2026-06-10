import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("folders", () => {
  test("the + button creates a folder and drops into inline rename", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("new-folder").click();
    const rename = page.locator('[data-testid^="folder-rename-"]');
    await expect(rename).toBeVisible();
    await rename.fill("QA Folder");
    await rename.press("Enter");
    await expect(
      page.locator('[data-testid^="folder-name-"]', { hasText: "QA Folder" }),
    ).toBeVisible();
  });

  test("collapse-all hides folder contents and persists across reload", async ({ page }) => {
    await signIn(page);
    // The seeded Technology folder holds feed-1; visible while expanded.
    await expect(page.getByTestId("feed-1")).toBeVisible();
    await page.getByTestId("toggle-collapse-all").click();
    await expect(page.getByTestId("feed-1")).toBeHidden();
    // Persisted in localStorage → still collapsed after a reload.
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect(page.getByTestId("feed-1")).toBeHidden();
    // Toggling again expands everything back.
    await page.getByTestId("toggle-collapse-all").click();
    await expect(page.getByTestId("feed-1")).toBeVisible();
  });
});
