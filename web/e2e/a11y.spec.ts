import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { signIn } from "./helpers";

// Runs axe-core against the major UI surfaces. We focus on the WCAG 2.1 AA
// rules (the same set every audit shop checks) plus 'best-practice' tags
// for non-blocking style nits.

test.describe("a11y", () => {
  test("login screen has no WCAG 2.1 AA violations", async ({ page }) => {
    await page.goto("/");
    await page.waitForSelector("[data-testid=login-submit]");
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
  });

  test("shell (signed in) has no WCAG 2.1 AA violations", async ({ page }) => {
    await signIn(page);
    await page.waitForSelector("[data-testid=article-list]");
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      // The story-thumb img has alt='' because it's decorative — axe is happy
      // with that, but third-party RSS images we render with the SAME alt
      // pattern shouldn't be flagged either.
      .analyze();
    expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
  });

  test("settings modal has no WCAG 2.1 AA violations", async ({ page }) => {
    await signIn(page);
    await page.locator("[data-user-chip]").click();
    await page.getByTestId("open-settings").click();
    await page.waitForSelector("[data-testid=settings]");
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
  });
});
