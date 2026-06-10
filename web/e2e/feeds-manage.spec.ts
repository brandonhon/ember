import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("feed management", () => {
  test("mute via the row's action menu — feed gets the muted visual + 'Unmute' becomes the menu choice", async ({ page }) => {
    await signIn(page);

    // Add a fresh feed so we don't disturb the seeded one used by other specs.
    await page.getByTestId("open-add-feed").click();
    await page.getByTestId("add-feed-input").fill("https://muteme.test/feed");
    await page.getByTestId("add-feed-submit").click();
    // add-feed runs feed discovery, which does a real DNS lookup of the fake
    // .test domain; that can take up to the discover client's ~10s timeout to
    // NXDOMAIN under full-suite load, so allow margin to keep this non-flaky.
    const row = page.locator("button", { hasText: "muteme.test" }).first();
    await expect(row).toBeVisible({ timeout: 15_000 });

    // Find the matching feed-row in the sidebar. The hover trigger only
    // appears on hover, so we use the underlying data attribute selector.
    const feedRow = page.locator(".feed-row", { hasText: "muteme.test" }).first();
    const trigger = feedRow.locator("[data-feed-actions-trigger]");
    await trigger.click({ force: true });

    // Mute it.
    const muteBtn = feedRow.locator("button", { hasText: /Mute/ }).first();
    await muteBtn.click();

    // The row gets the .muted class (visually grays the label). Generous
    // timeout: the PATCH round-trip + re-render can lag under full-suite load.
    await expect(feedRow).toHaveClass(/muted/, { timeout: 10_000 });

    // Reopen the menu — the option should now read "Unmute".
    await trigger.click({ force: true });
    await expect(feedRow.locator("button", { hasText: "Unmute" })).toBeVisible();
  });

  test("delete via the row's action menu — confirm dialog accepted, feed disappears", async ({ page }) => {
    await signIn(page);

    // Add a throwaway feed.
    await page.getByTestId("open-add-feed").click();
    await page.getByTestId("add-feed-input").fill("https://gone.test/feed");
    await page.getByTestId("add-feed-submit").click();
    // Same discover-DNS latency as the mute test above — generous timeout.
    const row = page.locator(".feed-row", { hasText: "gone.test" }).first();
    await expect(row).toBeVisible({ timeout: 15_000 });

    await row.locator("[data-feed-actions-trigger]").click({ force: true });
    await row.locator("button", { hasText: "Delete" }).click();

    // Confirm in the in-app ConfirmDialog (no more browser confirm()).
    await page.getByTestId("confirm-ok").click();

    await expect(page.locator(".feed-row", { hasText: "gone.test" })).toHaveCount(0);
  });

  test("sidebar can be collapsed and re-expanded from the topbar toggle", async ({ page }) => {
    await signIn(page);
    const rail = page.locator(".rail");
    await expect(rail).toBeVisible();

    await page.getByTestId("toggle-sidebar").click();
    await expect(rail).toBeHidden();

    await page.getByTestId("toggle-sidebar").click();
    await expect(rail).toBeVisible();
  });
});
