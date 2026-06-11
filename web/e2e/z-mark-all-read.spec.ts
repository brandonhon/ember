import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

// z- prefix: runs LAST in the serial, shared-DB suite. Marking every Fresh
// article read empties the Fresh list, so this must not precede any spec that
// needs unread Fresh content (e.g. viewport.spec's scroll test).
test.describe("fresh mark-all-read", () => {
  test("marking all read removes the cards from Fresh and reloads", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("view-fresh").click();

    const stories = page.locator('[data-testid^="story-"]');
    // Precondition: Fresh shows unread cards.
    await expect(stories.first()).toBeVisible();
    expect(await stories.count()).toBeGreaterThan(0);

    await page.getByTestId("mark-all-read").click();

    // Fresh lists only unread, so the just-read cards drop out on reload.
    // The seed fits in one page, so nothing pages in behind them → empty.
    await expect(stories).toHaveCount(0);
  });
});
