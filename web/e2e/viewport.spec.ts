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

  // HTML5 drag-and-drop produces no events from touch input, so the sidebar's
  // drag-to-file gesture is desktop-only. These two are how a touch user moves
  // a feed between folders; both must keep working.
  test("a feed can be refiled by tapping ⋯ → Move to folder", async ({ page }) => {
    await signIn(page);
    await page.setViewportSize({ width: 412, height: 839 });
    await page.locator('[aria-label="Open sidebar"]').click();

    const folderOf = () =>
      page.evaluate(() =>
        document
          .querySelector('[data-testid="feed-5"]')
          ?.closest(".folder")
          ?.querySelector(".folder-name")
          ?.textContent?.trim(),
      );
    const start = await folderOf();
    const dest = start === "World" ? "Technology" : "World";
    const id = await page.evaluate(
      (n: string) =>
        Array.from(document.querySelectorAll('[data-testid^="folder-name-"]'))
          .find((e) => e.textContent?.trim() === n)
          ?.getAttribute("data-testid")
          ?.replace("folder-name-", ""),
      dest,
    );

    await page.getByTestId("feed-actions-5").click();
    await page.getByTestId("feed-move-5").click();
    const panel = page.locator("[data-feed-move-for='5']");
    await expect(panel).toBeVisible();
    // It must fit the narrow rail rather than running off the edge.
    const box = (await panel.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(412);

    await page.getByTestId(`feed-move-target-5-${id}`).click();
    await expect.poll(folderOf).toBe(dest);
  });

  test("a feed can be refiled from Edit feed → Folder", async ({ page }) => {
    await signIn(page);
    await page.setViewportSize({ width: 412, height: 839 });
    await page.locator('[aria-label="Open sidebar"]').click();

    const folderOf = () =>
      page.evaluate(() =>
        document
          .querySelector('[data-testid="feed-5"]')
          ?.closest(".folder")
          ?.querySelector(".folder-name")
          ?.textContent?.trim(),
      );
    const start = await folderOf();
    const dest = start === "World" ? "Technology" : "World";

    await page.getByTestId("feed-actions-5").click();
    await page.getByTestId("feed-edit-5").click();
    await page.getByTestId("edit-feed-folder").selectOption({ label: dest });
    await page.getByTestId("edit-feed-save").click();

    await expect.poll(folderOf).toBe(dest);
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await page.locator('[aria-label="Open sidebar"]').click();
    await expect.poll(folderOf).toBe(dest);
  });
});
