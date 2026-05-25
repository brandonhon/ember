import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("article tags", () => {
  test("add and remove a tag on an article", async ({ page }) => {
    await signIn(page);
    // Pick the first article.
    await page.locator("[data-testid^=story-]").first().click();
    const input = page.getByTestId("tag-input");
    await input.fill("interesting");
    await input.press("Enter");
    const chip = page.locator("[data-testid=article-tags] .tag-chip", { hasText: "#interesting" });
    await expect(chip).toBeVisible();
    // Remove
    await chip.locator(".tag-chip-x").click();
    await expect(chip).toHaveCount(0);
  });
});
