import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("admin settings", () => {
  async function openFeedsSection(page: import("@playwright/test").Page) {
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByTestId("settings-feeds").click();
  }

  test("feed check interval can be changed and persists across reload", async ({ page }) => {
    await signIn(page);
    await openFeedsSection(page);

    const select = page.getByTestId("poll-interval");
    await expect(select).toBeVisible();
    await select.selectOption("3600"); // 1 hour (within the 5m–24h bounds)
    await page.getByTestId("poll-interval-save").click();

    // Reload and reopen — the saved value (persisted in app_settings) is
    // reflected, proving the GET/PATCH round-trip wired through.
    await page.reload();
    await openFeedsSection(page);
    await expect(page.getByTestId("poll-interval")).toHaveValue("3600");
  });
});
