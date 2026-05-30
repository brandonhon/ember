import { describe, it, expect, beforeEach, vi } from "vitest";
import { get } from "svelte/store";
import {
  activeView,
  articles,
  feeds,
  loadArticles,
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
  articles.set({ items: [], loading: false });
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
    });
    fetchMock.mockResolvedValueOnce(envelope({ count: 2 }));
    await setRead([10, 11], true);
    expect(get(articles).items.every((a) => a.is_read)).toBe(true);
    expect(get(feeds)[0].unread).toBe(3);
  });
});

describe("toggleStar", () => {
  it("flips is_starred locally", async () => {
    articles.set({ items: [article({ id: 42, is_starred: false })], loading: false });
    fetchMock.mockResolvedValueOnce(envelope({ ok: true }));
    await toggleStar(42, true);
    expect(get(articles).items[0].is_starred).toBe(true);
  });
});

describe("totalUnread", () => {
  it("sums across feeds", () => {
    feeds.set([feedRow({ id: 1, unread: 3 }), feedRow({ id: 2, unread: 2, url: "https://y.test/feed" })]);
    expect(get(totalUnread)).toBe(5);
  });
});

describe("refreshSmartCounts", () => {
  it("updates pending_summary from the server (drives the summarizing bar to zero)", async () => {
    smartCounts.set({ fresh: 0, starred: 0, later: 0, shared: 0, pending_summary: 7 });
    fetchMock.mockResolvedValueOnce(
      envelope({ fresh: 2, starred: 1, later: 0, shared: 0, pending_summary: 0 }),
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
});
