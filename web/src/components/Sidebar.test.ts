import { render, fireEvent } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach } from "vitest";
import Sidebar from "./Sidebar.svelte";
import { activeView, feeds, summariesEnabled } from "../lib/stores";
import type { FeedWithCounts } from "../lib/types";

const fetchMock = vi.fn();

function feedRow(over: Partial<FeedWithCounts> = {}): FeedWithCounts {
  return {
    id: 1,
    url: "https://x.test/feed",
    title: "Example Feed",
    fetch_interval: 1800,
    error_count: 0,
    created_at: 0,
    subscription_id: 7,
    muted: false,
    summarize: true,
    position: 0,
    unread: 3,
    ...over,
  };
}

function envelope(data: unknown) {
  return new Response(JSON.stringify({ data, meta: {} }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

beforeEach(() => {
  fetchMock.mockReset();
  // Everything the sidebar's refresh fan-out touches answers with an empty
  // envelope; the assertions below are about WHICH requests were made.
  fetchMock.mockImplementation((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url.includes("/api/stats/smart")) return Promise.resolve(envelope({}));
    return Promise.resolve(envelope([]));
  });
  globalThis.fetch = fetchMock;
  feeds.set([feedRow()]);
  activeView.set({ kind: "smart", view: "fresh" });
  summariesEnabled.set(true);
});

// Returns [method, url, body] for every fetch the component made.
function calls(): Array<[string, string, string]> {
  return fetchMock.mock.calls.map(([input, init]) => {
    const url = typeof input === "string" ? input : String(input);
    return [String(init?.method ?? "GET"), url, String(init?.body ?? "")];
  });
}

async function openFeedMenu(container: HTMLElement) {
  const more = container.querySelector('[aria-label="Feed actions"]');
  expect(more, "the feed's ⋯ button should render").not.toBeNull();
  await fireEvent.click(more as Element);
}

describe("Sidebar feed menu — AI summary opt-out", () => {
  it("sends the opt-out and re-fetches the article list, not just the sidebar", async () => {
    const { container, findByTestId } = render(Sidebar);
    await openFeedMenu(container);

    const toggle = await findByTestId("feed-summarize-1");
    expect(toggle).toHaveTextContent("Don't summarize");
    await fireEvent.click(toggle);

    // The handler is async (PATCH -> refreshSidebar -> loadArticles); a click
    // event only flushes one tick, so poll rather than assert immediately.
    await vi.waitFor(() => {
      const patch = calls().find(([m, u]) => m === "PATCH" && u.includes("/api/feeds/7"));
      expect(patch, `no PATCH to the subscription id; saw ${JSON.stringify(calls())}`).toBeDefined();
      expect(JSON.parse(patch![2])).toEqual({ summarize: false });

      // The point of the test. Unlike mute, opting out changes the ARTICLE
      // payload — summaries are blanked per-user server-side — so a
      // sidebar-only refresh would leave stale summary text on screen.
      expect(
        calls().some(([m, u]) => m === "GET" && u.includes("/api/articles")),
        `opting out must re-fetch the loaded articles, not only the sidebar; saw ${JSON.stringify(calls())}`,
      ).toBe(true);
    });
  });

  it("labels the entry 'Summarize' when the feed is already opted out", async () => {
    feeds.set([feedRow({ summarize: false })]);
    const { container, findByTestId } = render(Sidebar);
    await openFeedMenu(container);

    const toggle = await findByTestId("feed-summarize-1");
    expect(toggle).toHaveTextContent("Summarize");
    await fireEvent.click(toggle);

    await vi.waitFor(() => {
      const patch = calls().find(([m, u]) => m === "PATCH" && u.includes("/api/feeds/7"));
      expect(patch).toBeDefined();
      expect(JSON.parse(patch![2])).toEqual({ summarize: true });
    });
  });

  // Same guard as Resummarize: with summaries off server-wide the control has
  // nothing to act on. The stored flag is untouched — the entry just returns
  // when summaries are re-enabled.
  it("is absent when AI summaries are disabled server-wide", async () => {
    summariesEnabled.set(false);
    const { container, queryByTestId } = render(Sidebar);
    await openFeedMenu(container);

    // Precondition: the menu really did open, so the absence below means the
    // guard fired rather than the menu never rendering.
    expect(queryByTestId("feed-mute-1")).not.toBeNull();
    expect(queryByTestId("feed-summarize-1")).toBeNull();
    expect(queryByTestId("feed-resummarize-1")).toBeNull();
  });
});
