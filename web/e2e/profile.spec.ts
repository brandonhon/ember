import { test, expect, type Page } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("profile settings", () => {
  async function openSettings(page: Page) {
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
  }

  test("self-service email can be set and persists across reload", async ({ page }) => {
    await signIn(page);
    await openSettings(page);
    await page.getByTestId("settings-profile").click();

    const input = page.getByTestId("profile-email");
    await expect(input).toBeVisible();
    await input.fill("me@example.com");
    await page.getByTestId("profile-email-save").click();
    await expect(page.getByTestId("profile-email-msg")).toBeVisible();

    // Reload + reopen — the saved value comes back from /api/me, proving the
    // PATCH /me/email round-trip persisted.
    await page.reload();
    await openSettings(page);
    await page.getByTestId("settings-profile").click();
    await expect(page.getByTestId("profile-email")).toHaveValue("me@example.com");
  });

  test("an invalid email is rejected with a visible error", async ({ page }) => {
    await signIn(page);
    await openSettings(page);
    await page.getByTestId("settings-profile").click();

    await page.getByTestId("profile-email").fill("not-an-email");
    await page.getByTestId("profile-email-save").click();
    await expect(page.getByTestId("profile-email-err")).toBeVisible();
  });

  test("About version badge populates on a fresh login without a reload", async ({ page }) => {
    // Regression for the login() -> refreshMe() fix: a fresh password login
    // must populate appVersion, otherwise the badge renders empty until the
    // next page reload.
    await signIn(page);
    await openSettings(page);
    await page.getByTestId("settings-about").click();
    await expect(page.getByTestId("about-version")).not.toHaveText("");
  });
});
