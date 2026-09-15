import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("feeds", () => {
  test("seeded feed appears in sidebar with unread badge", async ({ page }) => {
    await signIn(page);
    const feedRow = page.getByTestId("feed-1");
    await expect(feedRow).toBeVisible();
    await expect(feedRow).toContainText("Example Tech Blog");
    // Seeded fixtures, none read yet — count > 0.
    const text = await feedRow.innerText();
    const n = Number(text.match(/(\d+)$/)?.[1] ?? 0);
    expect(n).toBeGreaterThan(0);
  });

  test("adding a feed URL via the sidebar adds a row", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("open-add-feed").click();
    await page.getByTestId("add-feed-input").fill("https://added.test/feed");
    await page.getByTestId("add-feed-submit").click();
    // add-feed runs discovery (real DNS lookup of the fake .test domain), which
    // can take up to the ~10s discover timeout under load — generous margin.
    await expect(page.locator("button", { hasText: "added.test" })).toBeVisible({
      timeout: 15_000,
    });
  });

  test("the add-feed form files the new feed in the chosen folder", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("open-add-feed").click();
    await page.getByTestId("add-feed-input").fill("https://filed.test/feed");
    // Pick "Design" by its option label rather than a hardcoded category id.
    await page.getByTestId("add-feed-folder").selectOption({ label: "Design" });
    await page.getByTestId("add-feed-submit").click();

    const design = page.locator("div.folder").filter({
      has: page.locator('[data-testid^="folder-name-"]', { hasText: "Design" }),
    });
    await expect(design.locator("button", { hasText: "filed.test" })).toBeVisible({
      timeout: 15_000,
    });
  });

  test("every feed picked from the multi-feed modal lands in the chosen folder", async ({ page }) => {
    await signIn(page);
    // Discovery needs a site that advertises several feeds; stub it so the
    // picker opens without depending on a real multi-feed host.
    await page.route("**/api/feeds/discover", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          data: {
            feeds: [
              { url: "https://multi.test/a.xml", title: "Section A" },
              { url: "https://multi.test/b.xml", title: "Section B" },
            ],
          },
        }),
      }),
    );
    const posted: Array<Record<string, unknown>> = [];
    page.on("request", (r) => {
      if (r.method() === "POST" && r.url().endsWith("/api/feeds")) {
        posted.push(JSON.parse(r.postData() ?? "{}"));
      }
    });

    await page.getByTestId("open-add-feed").click();
    await page.getByTestId("add-feed-input").fill("https://multi.test");
    await page.getByTestId("add-feed-folder").selectOption({ label: "Design" });
    await page.getByTestId("add-feed-submit").click();

    await expect(page.getByTestId("feed-picker")).toBeVisible();
    for (const item of await page.getByTestId("feed-picker-item").all()) {
      await item.locator("input[type=checkbox]").check();
    }
    await page.getByTestId("feed-picker-add").click();

    const design = page.locator("div.folder").filter({
      has: page.locator('[data-testid^="folder-name-"]', { hasText: "Design" }),
    });
    // The rows show the feed URL: the stub only drives discovery, so the real
    // fetch of the fake host fails and no title is ever stored.
    await expect(design.locator("button", { hasText: "multi.test/a.xml" })).toBeVisible({
      timeout: 15_000,
    });
    await expect(design.locator("button", { hasText: "multi.test/b.xml" })).toBeVisible();
    // Both adds carried the folder, not just the first.
    expect(posted).toHaveLength(2);
    const cats = new Set(posted.map((p) => p.category_id));
    expect(cats.size).toBe(1);
    expect([...cats][0]).toBeTruthy();
  });

  test("the add-feed folder defaults to the folder being viewed", async ({ page }) => {
    await signIn(page);
    await page.locator('[data-testid^="folder-name-"]', { hasText: "Design" }).click();
    await page.getByTestId("open-add-feed").click();
    await expect(page.getByTestId("add-feed-folder")).toHaveValue(
      await page
        .locator('[data-testid^="folder-name-"]', { hasText: "Design" })
        .evaluate((el) => el.getAttribute("data-testid")!.replace("folder-name-", "")),
    );
  });

  test("the feed menu can move a feed to another folder and out again", async ({ page }) => {
    await signIn(page);
    const folderOf = () =>
      page.evaluate(() =>
        document
          .querySelector('[data-testid="feed-5"]')
          ?.closest(".folder")
          ?.querySelector(".folder-name")
          ?.textContent?.trim(),
      );
    const catID = (name: string) =>
      page.evaluate(
        (n: string) =>
          Array.from(document.querySelectorAll('[data-testid^="folder-name-"]'))
            .find((e) => e.textContent?.trim() === n)
            ?.getAttribute("data-testid")
            ?.replace("folder-name-", ""),
        name,
      );

    await page.getByTestId("feed-actions-5").click();
    await page.getByTestId("feed-move-5").click();
    const panel = page.locator("[data-feed-move-for='5']");
    await expect(panel).toBeVisible();
    // The folder it's in is marked, so the panel says where it currently lives.
    await expect(panel.locator("button.current")).toHaveCount(1);

    await page.getByTestId(`feed-move-target-5-${await catID("World")}`).click();
    await expect.poll(folderOf).toBe("World");

    // 0 is "No folder" — the same clear_category path the drag-out uses.
    await page.getByTestId("feed-actions-5").click();
    await page.getByTestId("feed-move-5").click();
    await page.getByTestId("feed-move-target-5-0").click();
    await expect.poll(folderOf).toBe("Uncategorized");

    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await expect.poll(folderOf).toBe("Uncategorized");
  });

  test("clicking the seeded feed scopes the article list to that feed", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("feed-1").click();
    // The feed view is bounded by the reading window (24h), so the two recent
    // fixtures render and the 2-day-old one (story-3) is hidden — the same
    // window the Fresh view and the feed's unread badge use.
    await expect(page.getByTestId("story-1")).toBeVisible();
    await expect(page.getByTestId("story-2")).toBeVisible();
    await expect(page.getByTestId("story-3")).toHaveCount(0);
  });
});
