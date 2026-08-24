import { render } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import App from "./App.svelte";
import { user, smartCounts, summariesEnabled } from "./lib/stores";

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  globalThis.fetch = fetchMock;
});

describe("App", () => {
  it("renders the boot state, then logs the user out when /api/me 401s", async () => {
    // Boot fan-out makes several fetches (feeds, settings, etc.) in parallel
    // with /api/me; use mockResolvedValue (not Once) so every one returns 401
    // and the un-mocked ones can't return undefined and crash the test runner.
    // The mock factory returns a fresh Response per call since Response bodies
    // can only be read once.
    fetchMock.mockImplementation(() =>
      Promise.resolve(
        new Response('{"error":{"code":"unauthorized","message":"x"}}', { status: 401 }),
      ),
    );
    const { findByText } = render(App);
    // The Login screen should appear after mount + 401.
    const heading = await findByText("Ember");
    expect(heading).toBeInTheDocument();
  });
});

// The 15s poll refreshes smart counts whenever pending_summary is non-zero so
// the "Summarizing N…" footer counts down. With summaries disabled server-wide
// nothing drains that tally, so the condition stays true forever and the app
// re-requests a number that cannot move — once every 15s, for the whole session.
describe("App summary-countdown polling", () => {
  function envelope(data: unknown) {
    return new Response(JSON.stringify({ data, meta: {} }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }

  afterEach(() => {
    vi.useRealTimers();
    user.set(null);
    summariesEnabled.set(true);
  });

  // Returns how many times the app asked the server for smart counts.
  function smartCountCalls(): number {
    return fetchMock.mock.calls.filter(([input]) =>
      String(input).includes("/api/me/smart-counts"),
    ).length;
  }

  // Boots a logged-in session whose server reports summaries on/off, parks a
  // non-zero pending_summary, and returns how many smart-count refreshes the
  // next poll tick triggered.
  async function smartCountRefreshesPerTick(enabled: boolean): Promise<number> {
    fetchMock.mockImplementation((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/me") && !url.includes("smart-counts")) {
        return Promise.resolve(
          envelope({
            user: {
              id: 1, username: "alice", is_admin: false,
              settings_json: "{}", created_at: 0,
            },
            summaries_enabled: enabled,
          }),
        );
      }
      return Promise.resolve(envelope([]));
    });
    // Fake timers must be installed BEFORE render: startPolling's setInterval
    // has to be the fake one, or advanceTimersByTime can never fire it.
    vi.useFakeTimers();
    render(App);
    // Let mount, /api/me, and the sidebar fan-out settle so polling is running.
    await vi.advanceTimersByTimeAsync(0);
    smartCounts.set({
      fresh: 0, starred: 0, later: 0, shared: 0,
      pending_summary: 5, unread: 0, unread_by_category: {},
    });
    const before = smartCountCalls();
    await vi.advanceTimersByTimeAsync(15_000);
    return smartCountCalls() - before;
  }

  it("refreshes the count each tick while summaries are enabled", async () => {
    expect(await smartCountRefreshesPerTick(true)).toBeGreaterThan(0);
  });

  it("does not refresh the count when summaries are disabled server-wide", async () => {
    expect(await smartCountRefreshesPerTick(false)).toBe(0);
  });
});
