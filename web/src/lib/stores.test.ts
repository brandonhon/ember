import { describe, it, expect, beforeEach, vi } from "vitest";
import { get } from "svelte/store";
import {
  activeView,
  articles,
  feeds,
  loadArticles,
  loadMore,
  refreshSmartCounts,
  setRead,
  smartCounts,
  toggleStar,
  totalUnread,
  user,
} from "./stores";
import type { ArticleView, FeedWithCounts } from "./types";

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  globalThis.fetch = fetchMock;
  user.set(null);
  articles.set({ items: [], loading: false, hasMore: false });
  feeds.set([]);
  activeView.set({ kind: "smart", view: "fresh" });
});

function envelope<T>(data: T, meta: Record<string, unknown> = {}) {
  return new Response(JSON.stringify({ data, meta }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function article(over: Partial<ArticleView> = {}): ArticleView {
  return {
    id: 1,
    feed_id: 1,
    guid: "g1",
    title: "T",
    fetched_at: 0,
    content_hash: "h",
    is_read: false,
    is_starred: false,
    is_later: false,
    dup_count: 0,
    ...over,
  };
}

function feedRow(over: Partial<FeedWithCounts> = {}): FeedWithCounts {
  return {
    id: 1,
    url: "https://x.test/feed",
    title: "X",
    fetch_interval: 1800,
    error_count: 0,
    created_at: 0,
    subscription_id: 1,
    muted: false,
    position: 0,
    unread: 5,
    ...over,
  };
}

describe("loadArticles", () => {
  it("replaces items on first load and stores cursor", async () => {
    fetchMock.mockResolvedValueOnce(
      envelope([article({ id: 5, published_at: 1000 })], {
        next_cursor_pub: 1000,
        next_cursor_id: 5,
      }),
    );
    await loadArticles({ kind: "smart", view: "fresh" });
    const s = get(articles);
    expect(s.items.length).toBe(1);
    expect(s.cursor).toEqual({ pub: 1000, id: 5 });
    expect(s.loading).toBe(false);
  });

  it("appends with cursor on subsequent loads", async () => {
    articles.set({
      items: [article({ id: 1 })],
      loading: false,
      hasMore: true,
      cursor: { pub: 500, id: 1 },
    });
    fetchMock.mockResolvedValueOnce(envelope([article({ id: 2 })], {
      next_cursor_pub: 400,
      next_cursor_id: 2,
    }));
    await loadArticles({ kind: "smart", view: "fresh" }, true);
    expect(get(articles).items.map((a) => a.id)).toEqual([1, 2]);
    const [url] = fetchMock.mock.calls[0] as [string, unknown];
    expect(url).toContain("cursor_pub=500");
    expect(url).toContain("cursor_id=1");
  });

  it("clears hasMore when the page comes back without a cursor (last page)", async () => {
    fetchMock.mockResolvedValueOnce(envelope([article({ id: 7 })])); // no cursor meta
    await loadArticles({ kind: "smart", view: "fresh" });
    const s = get(articles);
    expect(s.hasMore).toBe(false);
    expect(s.cursor).toBeUndefined();
  });

  it("pages search by offset and stops when a short page returns", async () => {
    activeView.set({ kind: "search", query: "rust" });
    // First page: a full 25 results → hasMore, offset advances to 25.
    const full = Array.from({ length: 25 }, (_, i) => article({ id: i + 1, guid: `g${i + 1}` }));
    fetchMock.mockResolvedValueOnce(envelope(full));
    await loadArticles({ kind: "search", query: "rust" });
    let s = get(articles);
    expect(s.items.length).toBe(25);
    expect(s.hasMore).toBe(true);
    expect(s.searchOffset).toBe(25);

    // loadMore requests offset=25; a short page (2) ends paging.
    fetchMock.mockResolvedValueOnce(envelope([article({ id: 26 }), article({ id: 27 })]));
    await loadMore();
    s = get(articles);
    expect(s.items.length).toBe(27);
    expect(s.hasMore).toBe(false);
    const [url] = fetchMock.mock.calls[1] as [string, unknown];
    expect(url).toContain("offset=25");
    expect(url).toContain("limit=25");
  });

  it("captures errors without throwing", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response('{"error":{"code":"boom","message":"x"}}', { status: 500 }),
    );
    await loadArticles({ kind: "smart", view: "fresh" });
    expect(get(articles).err).toBeDefined();
    expect(get(articles).loading).toBe(false);
  });
});

describe("setRead", () => {
  it("optimistically updates items and decrements unread", async () => {
    feeds.set([feedRow({ id: 1, unread: 5 })]);
    articles.set({
      items: [article({ id: 10, feed_id: 1 }), article({ id: 11, feed_id: 1 })],
      loading: false,
      hasMore: false,
    });
    fetchMock.mockResolvedValueOnce(envelope({ count: 2 }));
    await setRead([10, 11], true);
    expect(get(articles).items.every((a) => a.is_read)).toBe(true);
    expect(get(feeds)[0].unread).toBe(3);
  });

  it("forwards include_siblings so reading a story sweeps its dedup copies", async () => {
    articles.set({ items: [article({ id: 10 })], loading: false, hasMore: false });
    fetchMock.mockResolvedValueOnce(envelope({ count: 1 }));
    await setRead([10], true, true);
    const body = JSON.parse((fetchMock.mock.calls.at(-1)![1] as RequestInit).body as string);
    expect(body).toMatchObject({ ids: [10], read: true, include_siblings: true });
  });
});

describe("toggleStar", () => {
  it("flips is_starred locally", async () => {
    articles.set({ items: [article({ id: 42, is_starred: false })], loading: false, hasMore: false });
    fetchMock.mockResolvedValueOnce(envelope({ ok: true }));
    await toggleStar(42, true);
    expect(get(articles).items[0].is_starred).toBe(true);
  });
});

describe("totalUnread", () => {
  it("uses the server's deduped count when present (positive)", () => {
    smartCounts.set({ fresh: 0, starred: 0, later: 0, shared: 0, pending_summary: 0, unread: 7, unread_by_category: {} });
    feeds.set([feedRow({ id: 1, unread: 3 }), feedRow({ id: 2, unread: 2, url: "https://y.test/feed" })]);
    expect(get(totalUnread)).toBe(7);
  });

  it("trusts a genuine server count of 0 over the non-deduped per-feed sum", () => {
    // Regression: when every in-window unread article is a cross-feed dedup
    // loser, the server's deduped All-Unread count is 0 while the per-feed
    // sum (no dedup) is positive. The badge must show 0 so it matches the
    // empty list — not the per-feed sum.
    smartCounts.set({ fresh: 0, starred: 0, later: 0, shared: 0, pending_summary: 0, unread: 0, unread_by_category: {} });
    feeds.set([feedRow({ id: 1, unread: 3 }), feedRow({ id: 2, unread: 2, url: "https://y.test/feed" })]);
    expect(get(totalUnread)).toBe(0);
  });

  it("falls back to summing per-feed counts when an older server omits unread", () => {
    // Older builds return no `unread` key at all (undefined, not 0).
    smartCounts.set({ fresh: 0, starred: 0, later: 0, shared: 0, pending_summary: 0, unread_by_category: null } as never);
    feeds.set([feedRow({ id: 1, unread: 3 }), feedRow({ id: 2, unread: 2, url: "https://y.test/feed" })]);
    expect(get(totalUnread)).toBe(5);
  });
});

describe("refreshSmartCounts", () => {
  it("updates pending_summary from the server (drives the summarizing bar to zero)", async () => {
    smartCounts.set({ fresh: 0, starred: 0, later: 0, shared: 0, pending_summary: 7, unread: 0, unread_by_category: {} });
    fetchMock.mockResolvedValueOnce(
      envelope({ fresh: 2, starred: 1, later: 0, shared: 0, pending_summary: 0, unread: 0, unread_by_category: {} }),
    );
    await refreshSmartCounts();
    const sc = get(smartCounts);
    expect(sc.pending_summary).toBe(0);
    expect(sc.fresh).toBe(2);
    // Only the smart-counts endpoint should have been hit (not the full sidebar).
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url] = fetchMock.mock.calls[0] as [string, unknown];
    expect(url).toContain("/api/me/smart-counts");
  });

  it("a stale in-flight response cannot clobber a newer one", async () => {
    const counts = (unread: number) =>
      envelope({ fresh: 0, starred: 0, later: 0, shared: 0, pending_summary: 0, unread, unread_by_category: {} });
    // First refresh (stale unread=53) resolves LATE; second (fresh unread=0)
    // resolves first — mirrors a poll-issued count landing after a post-mark
    // refresh. The older response must be dropped.
    let resolveStale!: (r: Response) => void;
    const stale = new Promise<Response>((r) => (resolveStale = r));
    fetchMock.mockReturnValueOnce(stale).mockResolvedValueOnce(counts(0));

    const p1 = refreshSmartCounts(); // seq N, pending
    const p2 = refreshSmartCounts(); // seq N+1, resolves now
    await p2;
    expect(get(smartCounts).unread).toBe(0);

    resolveStale(counts(53));
    await p1;
    expect(get(smartCounts).unread).toBe(0); // stale 53 ignored
  });
});
