import { render, fireEvent } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach } from "vitest";
import Sidebar from "./Sidebar.svelte";
import { activeView, feeds, smartCounts, summariesEnabled } from "../lib/stores";
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
    summarize_mode: "",
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
    expect(toggle).toHaveTextContent("Summaries: never");
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

  it("offers to turn summaries back on when the feed is already opted out", async () => {
    feeds.set([feedRow({ summarize: false })]);
    const { container, findByTestId } = render(Sidebar);
    await openFeedMenu(container);

    const toggle = await findByTestId("feed-summarize-1");
    expect(toggle).toHaveTextContent("Summaries: turn back on");
    await fireEvent.click(toggle);

    await vi.waitFor(() => {
      const patch = calls().find(([m, u]) => m === "PATCH" && u.includes("/api/feeds/7"));
      expect(patch).toBeDefined();
      expect(JSON.parse(patch![2])).toEqual({ summarize: true });
    });
  });

  // Picking a mode is also an opt-in: choosing WHEN a feed gets summarized
  // only makes sense if it gets summarized at all, so a feed the user had
  // turned off comes back on rather than storing a mode that does nothing.
  it("sends summarize: true alongside the mode, so picking one opts back in", async () => {
    feeds.set([feedRow({ summarize: false })]);
    const { container, findByTestId } = render(Sidebar);
    await openFeedMenu(container);

    await fireEvent.click(await findByTestId("feed-mode-on-demand-1"));

    await vi.waitFor(() => {
      const patch = calls().find(([m, u]) => m === "PATCH" && u.includes("/api/feeds/7"));
      expect(patch, `no PATCH; saw ${JSON.stringify(calls())}`).toBeDefined();
      expect(JSON.parse(patch![2])).toEqual({ summarize_mode: "on_demand", summarize: true });
    });
  });

  // The tick has to track BOTH the mode and the opt-out: an opted-out feed
  // still has a stored mode, and showing it as the active choice would claim
  // summaries are happening when none are.
  it("ticks the active mode only while the feed is opted in", async () => {
    feeds.set([feedRow({ summarize: true, summarize_mode: "all" })]);
    const { container, findByTestId, unmount } = render(Sidebar);
    await openFeedMenu(container);
    expect(await findByTestId("feed-mode-all-1")).toHaveTextContent("✓");
    expect(await findByTestId("feed-mode-inherit-1")).not.toHaveTextContent("✓");
    unmount();

    feeds.set([feedRow({ summarize: false, summarize_mode: "all" })]);
    const off = render(Sidebar);
    await openFeedMenu(off.container);
    expect(await off.findByTestId("feed-mode-all-1")).not.toHaveTextContent("✓");
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

// The pending-summary count is a plain "articles with no summary_model" tally;
// it is non-zero on any server that ingested articles before summaries were
// turned off. With no summarizer running there is no worker to drain it, so
// the footer would sit there claiming work forever.
describe("Sidebar summarizing footer", () => {
  const counts = (pending: number) => ({
    fresh: 0, starred: 0, later: 0, shared: 0,
    pending_summary: pending, unread: 0, unread_by_category: {},
  });

  it("shows the count while summaries are enabled", async () => {
    smartCounts.set(counts(5));
    const { queryByTestId } = render(Sidebar);
    const label = queryByTestId("sidebar-summarizing")?.textContent?.replace(/\s+/g, " ");
    expect(label).toContain("Summarizing 5 articles");
  });

  it("is absent when AI summaries are disabled server-wide", async () => {
    summariesEnabled.set(false);
    smartCounts.set(counts(5));
    const { queryByTestId } = render(Sidebar);
    expect(queryByTestId("sidebar-summarizing")).toBeNull();
  });
});
