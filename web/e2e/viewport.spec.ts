import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

// Mobile single-pane layout. Named to sort last in the (serial, shared-DB)
// suite because opening an article marks it read; running after the
// read-state-sensitive specs (reading.spec) keeps their assertions intact.
test.describe("mobile viewport", () => {
  test("list scroll position survives a reader round-trip", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("view-fresh").click();
    // Narrow + tall enough that the Fresh list overflows (a non-zero scroll
    // position to preserve) and the layout is in single-pane mobile mode.
    await page.setViewportSize({ width: 375, height: 700 });

    const list = page.locator(".list-col");
    await expect(list).toBeVisible();

    const scrollable = await list.evaluate((el) => el.scrollHeight - el.clientHeight);
    expect(scrollable).toBeGreaterThan(20);

    // Scroll down a little; this is the position that must be restored.
    await list.evaluate((el) => (el.scrollTop = 40));
    const before = await list.evaluate((el) => el.scrollTop);
    expect(before).toBe(40);

    // Open a story via dispatchEvent so Playwright's actionability scroll (which
    // would reset scrollTop) and the sticky-header overlay don't interfere.
    await page.locator('[data-testid^="story-"]').first().locator(".story-link").dispatchEvent("click");
    await expect(page.locator(".reader")).toBeVisible();
    // The list stays MOUNTED while hidden — the bug was that it unmounted,
    // losing scroll position, and remounting reset it to the top.
    await expect(list).toBeAttached();
    await expect(list).toBeHidden();

    // Tap back → the list returns at the SAME scroll position, not the top.
    await page.getByTestId("mobile-back").click();
    await expect(list).toBeVisible();
    expect(await list.evaluate((el) => el.scrollTop)).toBe(before);
  });
});
