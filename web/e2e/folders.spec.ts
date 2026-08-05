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

  test("dragging a feed onto a feed row in another folder moves it", async ({ page }) => {
    await signIn(page);
    const folder = (name: string) =>
      page.locator("div.folder").filter({
        has: page.locator('[data-testid^="folder-name-"]', { hasText: name }),
      });
    const design = folder("Design");
    const tech = folder("Technology");

    const dragged = design.locator("[data-testid^='feed-']").first();
    const id = await dragged.getAttribute("data-testid");
    await dragged.dragTo(tech.locator("[data-testid^='feed-']").first());

    // No reload: the optimistic store update must already show the move. This
    // regressed once because the drop handler read the drag ref after an await,
    // by which point dragend had cleared it.
    await expect(tech.locator(`[data-testid='${id}']`)).toBeVisible();
    await expect(design.locator(`[data-testid='${id}']`)).toHaveCount(0);

    // ...and the server persisted it.
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect(tech.locator(`[data-testid='${id}']`)).toBeVisible();
  });
});
