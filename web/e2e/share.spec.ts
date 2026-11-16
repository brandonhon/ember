import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("share article", () => {
  test("the Copy link tab copies the link in one click", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await signIn(page);
    await page.locator("[data-testid^=story-]").first().click();
    await page.getByTestId("reader-share").click();
    await page.getByTestId("share-tab-link").click();

    const url = await page.getByTestId("share-link-input").inputValue();
    expect(url).toMatch(/^https?:\/\//);
    await expect(page.getByTestId("share-link-msg")).toHaveText("Link copied to clipboard");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(url);
    // No second copy button to click.
    await expect(page.getByTestId("share-link-copy")).toHaveCount(0);
  });
});
