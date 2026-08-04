import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

// The summary grace window (#162) bounds how long an article stays hidden
// waiting for its AI summary. The control is deliberately scoped to the
// Language model section's summaries-ENABLED branch, because the setting is
// meaningless when nothing is summarizing.
//
// The e2e server runs in test mode, where the summarizer is a noop and
// `d.Ollama` is nil — so summaries report disabled here. That makes this suite
// the right place to pin the *hidden* half of the requirement; the save path
// is covered by the Go admin-settings tests, which can drive it directly.
test.describe("summary grace window", () => {
  test("is hidden when AI summaries are disabled", async ({ page }) => {
    await signIn(page);
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByTestId("settings-llm").click();

    // Precondition: this instance really does report summaries off. Without
    // this the absence below would prove nothing — the control could be
    // missing for any reason.
    await expect(page.getByText(/Summaries are disabled on this server/i)).toBeVisible();

    await expect(page.getByTestId("summary-grace")).toHaveCount(0);
    await expect(page.getByTestId("summary-grace-save")).toHaveCount(0);
  });
});
