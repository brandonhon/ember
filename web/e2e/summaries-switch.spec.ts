import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

// Regression test for issue #198: the reporter thought they'd turned
// summaries off, but the switch they flipped (Reading → AI summary card) is
// a per-user display preference that never stopped inference — leaving a
// permanently stuck "Summarizing 314 articles…" indicator with no way to
// diagnose or recover from the UI.
//
// This covers the half of the fix that only a real browser can prove: that
// the new Settings → Language model → Summaries switch is reachable and that
// switching it off is a durable, server-side change — not local component
// state that a reload would erase. The Go admin-settings tests cover the
// PATCH round-trip and the drain/requeue counts directly.
test.describe("summaries switch", () => {
  async function openLLMSection(page: import("@playwright/test").Page) {
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    await page.getByTestId("settings-llm").click();
  }

  test.afterEach(async ({ page }) => {
    // The trailing click in the test body only runs if every assertion
    // before it passes — a mid-test failure would otherwise leave summaries
    // off for every later spec, since the server and database are shared.
    await page.getByTestId("summaries-on").click().catch(() => {});
  });

  test("switching summaries off persists across a reload (#198)", async ({ page }) => {
    await signIn(page);
    await openLLMSection(page);

    // The switch must be visible and usable here regardless of whether a
    // summarizer backend is wired up — it is the only way back to "on".
    await expect(page.getByTestId("summaries-off")).toBeVisible();

    await page.getByTestId("summaries-off").click();
    await expect(page.getByTestId("summaries-off")).toHaveClass(/on/);

    // Reload — the whole point. This must not be local component state.
    await page.reload();
    await openLLMSection(page);
    await expect(page.getByTestId("summaries-off")).toHaveClass(/on/);

    // Leave the server as we found it for any other suite that runs after.
    await page.getByTestId("summaries-on").click();
    await expect(page.getByTestId("summaries-on")).toHaveClass(/on/);
  });
});
