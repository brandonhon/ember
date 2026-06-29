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

  async function openDatabaseSection(page: import("@playwright/test").Page) {
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByTestId("settings-database").click();
  }

  test("backup directory can be changed and persists across reload", async ({ page }) => {
    await signIn(page);
    await openDatabaseSection(page);

    const dir = page.getByTestId("db-backup-dir");
    await expect(dir).toBeVisible();
    await dir.fill("/mnt/ember-backups");
    await page.getByTestId("db-schedule-save").click();

    await page.reload();
    await openDatabaseSection(page);
    await expect(page.getByTestId("db-backup-dir")).toHaveValue("/mnt/ember-backups");
  });

  test("OPML import reports its own status, independent of the TT-RSS section", async ({ page }) => {
    await signIn(page);
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByTestId("settings-import").click();

    // The input is hidden; setInputFiles (relative to the web/ cwd) drives the
    // change handler directly without opening the native dialog.
    await page
      .getByTestId("opml-file-input")
      .setInputFiles("e2e/fixtures/sample-import.opml");

    // Feedback lands in the OPML card (its own status), not a frozen screen…
    await expect(
      page.locator("[data-testid=opml-msg], [data-testid=opml-error]"),
    ).toBeVisible({ timeout: 15_000 });
    // …and the TT-RSS card's status is never touched by an OPML import.
    await expect(page.getByTestId("import-msg")).toHaveCount(0);
    await expect(page.getByTestId("import-error")).toHaveCount(0);
  });
});
