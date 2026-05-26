import { render } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach } from "vitest";
import App from "./App.svelte";

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  global.fetch = fetchMock;
});

describe("App", () => {
  it("renders the boot state, then logs the user out when /api/me 401s", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response('{"error":{"code":"unauthorized","message":"x"}}', { status: 401 }),
    );
    const { findByText } = render(App);
    // The Login screen should appear after mount + 401.
    const heading = await findByText("Ember");
    expect(heading).toBeInTheDocument();
  });
});
