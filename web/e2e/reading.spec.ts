import { test, expect } from "@playwright/test";
import { signIn } from "./helpers";

test.describe("reading", () => {
  test("opening an article reveals the reader pane with its content", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("story-1").click();
    // Title rendered in the reader.
    await expect(page.locator("h1", { hasText: "First fixture article" })).toBeVisible();
    // Summary card from the seed.
    await expect(page.getByTestId("summary-card")).toBeVisible();
    await expect(page.getByTestId("summary-card")).toContainText("Test summary point one");
  });

  test("star toggle persists across reload", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("story-1").click();
    const starBtn = page.getByTestId("reader-star");
    await expect(starBtn).toBeVisible();
    await expect(starBtn).toContainText("Star");

    await starBtn.click();
    await expect(starBtn).toContainText("Starred");

    await page.reload();
    // After reload we need to log in via cookie (the session cookie is still
    // present in the browser context). Article list comes up, click the
    // starred story again.
    await expect(page.getByTestId("article-list")).toBeVisible();
    await page.getByTestId("story-1").click();
    await expect(page.getByTestId("reader-star")).toContainText("Starred");
  });

  test("Fresh smart view filters out older articles", async ({ page }) => {
    await signIn(page);
    // Sidebar defaults to Fresh on load; the seeded fixtures with published_at
    // within the last hour appear, the 2-day-old one does not.
    await page.getByTestId("view-fresh").click();
    await expect(page.getByTestId("story-1")).toBeVisible();
    await expect(page.getByTestId("story-2")).toBeVisible();
    await expect(page.getByTestId("story-3")).toHaveCount(0);
  });

  test("the m keyboard shortcut marks the selected article read", async ({ page }) => {
    await signIn(page);
    await page.getByTestId("feed-1").click();
    await expect(page.getByTestId("story-1")).toBeVisible();

    const initialText = await page.getByTestId("feed-1").innerText();
    const initial = Number(initialText.match(/(\d+)$/)?.[1] ?? 0);

    // Select the first article and toggle read via the documented keyboard
    // shortcut. This exercises the same setRead pipeline as scroll-to-read.
    await page.getByTestId("story-1").click();
    await page.keyboard.press("m");

    await expect.poll(
      async () => {
        const text = await page.getByTestId("feed-1").innerText();
        const m = text.match(/(\d+)$/);
        return m ? Number(m[1]) : 999;
      },
      { timeout: 5_000 },
    ).toBeLessThan(initial);
  });

  test("scrolling past articles marks them read (badge decrements)", async ({ page, viewport }) => {
    // Use a short viewport so even a couple of cards overflow the article
    // list container — gives the IntersectionObserver real geometry to work
    // with.
    await page.setViewportSize({ width: viewport?.width ?? 1280, height: 360 });

    await signIn(page);
    await page.getByTestId("feed-1").click();
    await expect(page.getByTestId("story-1")).toBeVisible();

    const initialText = await page.getByTestId("feed-1").innerText();
    const initial = Number(initialText.match(/(\d+)$/)?.[1] ?? 0);
    if (initial < 1) {
      test.skip(true, "no unread articles to decrement");
      return;
    }

    // Stepped scroll through the entire list so each card crosses the
    // visible boundary at least once and IO fires for every transition.
    await page.evaluate(async () => {
      const el = document.querySelector<HTMLElement>('[data-testid="article-list"]');
      if (!el) return;
      const target = el.scrollHeight;
      for (let y = 0; y <= target; y += 20) {
        el.scrollTop = y;
        await new Promise((r) => requestAnimationFrame(() => r(null)));
      }
    });

    await expect.poll(
      async () => {
        const text = await page.getByTestId("feed-1").innerText();
        const m = text.match(/(\d+)$/);
        return m ? Number(m[1]) : 999;
      },
      { timeout: 5_000 },
    ).toBeLessThan(initial);
  });
});
