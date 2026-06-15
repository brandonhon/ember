import { test, expect, type Page } from "@playwright/test";
import { signIn } from "./helpers";

// z- prefix: runs late in the serial, shared-DB suite. Opening an article marks
// its whole cross-feed dedup cluster read, so this must not precede specs that
// need those stories unread. Self-contained: it resets the target cluster to
// unread via the API before exercising the UI, so it's order-independent.
//
// Guards the duplicate-on-open regression: the seed syndicates one wire story
// across Reuters + The Verge (same title fingerprint). Fresh shows only the
// lowest-id winner; the sibling is suppressed. Because the dedup suppressor only
// hides UNREAD copies, marking just the visible winner read would un-hide the
// sibling, which the 15s poll then prepends as a phantom unread duplicate.
// Opening a story must mark its whole cluster read so that can't happen.
test.describe("cross-feed dedup: opening a story", () => {
  // Resolve the "OpenAI … compute deal" wire-story cluster from Fresh, force the
  // whole cluster unread (deterministic start), and return the winner + sibling
  // ids. Uses the page's own cookie + CSRF, like the other API-driven specs.
  async function resetComputeDealCluster(page: Page) {
    return page.evaluate(async () => {
      const csrf = decodeURIComponent(
        document.cookie.match(/(?:^|;\s*)ember_csrf=([^;]+)/)?.[1] ?? "",
      );
      // all=1 returns the deduped winner regardless of read state (Fresh would
      // miss it once an earlier spec in the shared-DB suite has read it).
      const all = await fetch("/api/articles?all=1&limit=200", { credentials: "include" });
      const items = (await all.json()).data ?? [];
      const winner = items.find((a: { title: string }) => /compute deal/i.test(a.title));
      if (!winner) return null;
      const c = await fetch(`/api/articles/${winner.id}/cluster`, { credentials: "include" });
      const siblings: { article_id: number }[] = (await c.json()).data?.siblings ?? [];
      const ids = [winner.id, ...siblings.map((s) => s.article_id)];
      await fetch("/api/articles/read", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Ember-CSRF": csrf },
        body: JSON.stringify({ ids, read: false }),
      });
      return { winnerId: winner.id as number, siblingCount: siblings.length };
    });
  }

  // Read flags of the winner's cross-feed siblings.
  async function siblingReadFlags(page: Page, winnerId: number): Promise<boolean[]> {
    return page.evaluate(async (id) => {
      const c = await fetch(`/api/articles/${id}/cluster`, { credentials: "include" });
      const siblings: { is_read: boolean }[] = (await c.json()).data?.siblings ?? [];
      return siblings.map((s) => s.is_read);
    }, winnerId);
  }

  test("opening a duplicated story marks its hidden cross-feed copy read too", async ({ page }) => {
    await signIn(page);
    const cluster = await resetComputeDealCluster(page);
    expect(cluster, "seed must include the OpenAI compute-deal dup pair").not.toBeNull();
    expect(cluster!.siblingCount).toBeGreaterThan(0);

    await page.reload();
    await expect(page.getByTestId("article-list")).toBeVisible();
    await page.getByTestId("view-fresh").click();

    // Deduped: the wire story shows exactly once (the lowest-id winner).
    const card = page.getByTestId(`story-${cluster!.winnerId}`);
    await expect(card).toBeVisible();

    // Open it — the reader marks the article read, and (the fix) its siblings.
    await card.click();
    await expect(page.locator("h1", { hasText: /compute deal/i })).toBeVisible();

    // The hidden sibling must now be read, so it can't resurface as a phantom
    // unread duplicate on the next poll. (setRead on open is fire-and-forget, so
    // poll until it lands.)
    await expect
      .poll(async () => {
        const flags = await siblingReadFlags(page, cluster!.winnerId);
        return flags.length > 0 && flags.every(Boolean);
      })
      .toBe(true);
  });
});
