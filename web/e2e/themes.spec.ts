import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("themes", () => {
  test("switching to Nord sets the document data-theme attribute", async ({ page }) => {
    await signIn(page);
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    // Settings opens on Profile; click Preferences in the side nav. The
    // button doesn't have a stable data-testid, so click by label.
    await page.getByRole("button", { name: "Preferences" }).click();
    await page.getByTestId("theme-nord").click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "nord");
  });

  test("auto mode resolves to light or dark based on prefers-color-scheme", async ({ page }) => {
    await signIn(page);
    await page.emulateMedia({ colorScheme: "dark" });
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByRole("button", { name: "Preferences" }).click();
    await page.getByTestId("theme-auto").click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await page.emulateMedia({ colorScheme: "light" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  });
});
