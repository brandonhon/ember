import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("folders", () => {
  test("the + button creates a folder and drops into inline rename", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("new-folder").click();
    const rename = page.locator('[data-testid^="folder-rename-"]');
    await expect(rename).toBeVisible();
    await rename.fill("QA Folder");
    await rename.press("Enter");
    await expect(
      page.locator('[data-testid^="folder-name-"]', { hasText: "QA Folder" }),
    ).toBeVisible();
  });

  test("collapse-all hides folder contents and persists across reload", async ({ page }) => {
    await signIn(page);
    // The seeded Technology folder holds feed-1; visible while expanded.
    await expect(page.getByTestId("feed-1")).toBeVisible();
    await page.getByTestId("toggle-collapse-all").click();
    await expect(page.getByTestId("feed-1")).toBeHidden();
    // Persisted in localStorage → still collapsed after a reload.
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect(page.getByTestId("feed-1")).toBeHidden();
    // Toggling again expands everything back.
    await page.getByTestId("toggle-collapse-all").click();
    await expect(page.getByTestId("feed-1")).toBeVisible();
  });

  test("dragging a feed onto a feed row in another folder moves it", async ({ page }) => {
    await signIn(page);
    const folder = (name: string) =>
      page.locator("div.folder").filter({
        has: page.locator('[data-testid^="folder-name-"]', { hasText: name }),
      });
    const design = folder("Design");
    const tech = folder("Technology");

    const dragged = design.locator("[data-testid^='feed-']").first();
    const id = await dragged.getAttribute("data-testid");
    await dragged.dragTo(tech.locator("[data-testid^='feed-']").first());

    // No reload: the optimistic store update must already show the move. This
    // regressed once because the drop handler read the drag ref after an await,
    // by which point dragend had cleared it.
    await expect(tech.locator(`[data-testid='${id}']`)).toBeVisible();
    await expect(design.locator(`[data-testid='${id}']`)).toHaveCount(0);

    // ...and the server persisted it.
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect(tech.locator(`[data-testid='${id}']`)).toBeVisible();
  });

  test("dropping a feed in a folder's empty space moves it into that folder", async ({ page }) => {
    await signIn(page);
    const folder = (name: string) =>
      page.locator("div.folder").filter({
        has: page.locator('[data-testid^="folder-name-"]', { hasText: name }),
      });
    // Explicit ids: the whole file shares one database serially, so an earlier
    // test has already emptied Design. feed-1 is seeded into Technology and
    // nothing before this moves it; World is untouched.
    const id = "feed-1";
    const dragged = page.getByTestId(id);

    // Not the header and not a row — the gutter beside the rows, which is where
    // a natural "drop it in the folder" gesture lands. This used to do nothing.
    const list = folder("World").locator(".feed-list");
    const to = (await list.boundingBox())!;
    const from = (await dragged.boundingBox())!;
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(from.x + from.width / 2, from.y - 10, { steps: 5 });
    await page.mouse.move(to.x + 4, to.y + to.height - 3, { steps: 20 });
    await page.mouse.up();

    await expect(folder("World").locator(`[data-testid='${id}']`)).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect(folder("World").locator(`[data-testid='${id}']`)).toBeVisible();
  });

  test("a feed can be dragged out of a folder onto Uncategorized", async ({ page }) => {
    await signIn(page);
    const uncatHead = page.locator(".folder-head").filter({ hasText: "Uncategorized" });
    // When nothing is uncategorized the zone doesn't exist at rest — it appears
    // for the duration of a feed drag, which is the only way out of a folder.
    // Running the whole suite, feeds.spec.ts has already added an uncategorized
    // feed, so don't assume either starting state; assert it's there once the
    // drag is under way.
    const dragged = page.getByTestId("feed-5");
    const from = (await dragged.boundingBox())!;
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(from.x + from.width / 2, from.y - 12, { steps: 6 });
    await expect(uncatHead).toBeVisible();
    // toBeVisible() is satisfied by an off-screen element, and once other specs
    // have added feeds the zone renders below the fold — the pointer could
    // never reach it. Scroll it in (the drag survives) before taking its box.
    await uncatHead.scrollIntoViewIfNeeded();

    const to = (await uncatHead.boundingBox())!;
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 20 });
    await page.mouse.move(to.x + to.width / 2 + 2, to.y + to.height / 2, { steps: 4 });
    await page.mouse.up();

    const folderOf = () =>
      page.evaluate(() =>
        document
          .querySelector('[data-testid="feed-5"]')
          ?.closest(".folder")
          ?.querySelector(".folder-name")
          ?.textContent?.trim(),
      );
    await expect.poll(folderOf).toBe("Uncategorized");
    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect.poll(folderOf).toBe("Uncategorized");
  });
});
