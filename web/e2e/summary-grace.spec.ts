import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

// The summary grace window (#162) bounds how long an article stays hidden
// waiting for its AI summary. The control (and its give-up-after sibling) is
// deliberately scoped to the Language model section's summaries-ENABLED
// branch, because the setting is meaningless once summaries are switched off
// — even with a backend configured.
//
// The e2e server runs in test mode with a noop summarizer wired up as a real
// backend, so `llm.enabled` is true here and the grace control renders by
// default. That means the disabled state has to be driven explicitly through
// the Summaries switch this branch added, rather than relied on as an
// incidental property of test mode.
test.describe("summary grace window", () => {
  test.afterEach(async ({ page }) => {
    // Leave the server as we found it for any suite that runs after this one
    // (specs share a single server and database) — even if an assertion
    // above failed mid-test.
    await page.getByTestId("summaries-on").click().catch(() => {});
  });

  test("is hidden when AI summaries are disabled", async ({ page }) => {
    await signIn(page);
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByTestId("settings-llm").click();

    // Precondition: the control is present while summaries are on. Without
    // this the absence below would prove nothing — the control could be
    // missing for any reason.
    await expect(page.getByTestId("summary-grace")).toBeVisible();

    await page.getByTestId("summaries-off").click();
    await expect(page.getByTestId("summaries-off")).toHaveClass(/on/);

    await expect(page.getByTestId("summary-grace")).toHaveCount(0);
    await expect(page.getByTestId("summary-grace-save")).toHaveCount(0);

    // Restore: switching summaries back on brings the control back.
    await page.getByTestId("summaries-on").click();
    await expect(page.getByTestId("summaries-on")).toHaveClass(/on/);
    await expect(page.getByTestId("summary-grace")).toBeVisible();
  });
});
