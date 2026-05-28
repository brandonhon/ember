import { render } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach } from "vitest";
import App from "./App.svelte";

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
