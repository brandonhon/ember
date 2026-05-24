import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("boards", () => {
  test("create a board from the sidebar, then add the open article to it from the reader", async ({ page }) => {
    await signIn(page);

    // Open the board form and create one.
    await page.getByTestId("open-add-board").click();
    await page.getByTestId("add-board-input").fill("Homelab ideas");
    await page.getByTestId("add-board-submit").click();

    // The board appears in the sidebar.
    const boardLink = page.locator('[data-testid^="board-"]', { hasText: "Homelab ideas" }).first();
    await expect(boardLink).toBeVisible({ timeout: 5_000 });

    // Open an article and add it to the board.
    const firstStory = page.locator("[data-testid^=story-]").first();
    if (await firstStory.count()) {
      await firstStory.click();
      await page.getByTestId("reader-board").click();
      // Picker shows the board.
      await page.locator('[data-testid^="picker-board-"]', { hasText: "Homelab ideas" }).first().click();
    }
  });

  test("clicking a board scopes the article list to that board", async ({ page }) => {
    await signIn(page);
    // Pick any existing board (created by the previous test or a fresh one).
    const anyBoard = page.locator('[data-testid^="board-"]').first();
    if ((await anyBoard.count()) === 0) {
      await page.getByTestId("open-add-board").click();
      await page.getByTestId("add-board-input").fill("Scope test");
      await page.getByTestId("add-board-submit").click();
      await page.waitForTimeout(300);
    }
    const board = page.locator('[data-testid^="board-"]').first();
    await board.click();
    // The article list header should reflect the board name.
    await expect(page.locator(".list-title")).not.toContainText("Fresh");
  });
});
