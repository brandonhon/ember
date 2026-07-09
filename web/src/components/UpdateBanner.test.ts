import { render, fireEvent } from "@testing-library/svelte";
import { describe, it, expect, beforeEach } from "vitest";
import UpdateBanner from "./UpdateBanner.svelte";
import { updateInfo } from "../lib/stores";
import type { UpdateInfo } from "../lib/types";

function info(over: Partial<UpdateInfo> = {}): UpdateInfo {
  return {
    current: "v0.9.4",
    latest: "v0.9.5",
    available: true,
    url: "https://github.com/brandonhon/ember/releases/tag/v0.9.5",
    published: "2026-07-01T00:00:00Z",
    checked_at: "2026-07-09T00:00:00Z",
    ...over,
  };
}

beforeEach(() => {
  localStorage.clear();
  updateInfo.set(null);
});

describe("UpdateBanner", () => {
  it("shows nothing when there is no update info", () => {
    const { queryByTestId } = render(UpdateBanner);
    expect(queryByTestId("update-banner")).toBeNull();
  });

  it("shows nothing when a result exists but no update is available", () => {
    updateInfo.set(info({ available: false, latest: "v0.9.4" }));
    const { queryByTestId } = render(UpdateBanner);
    expect(queryByTestId("update-banner")).toBeNull();
  });

  it("shows the banner with the release link when an update is available", async () => {
    updateInfo.set(info());
    const { findByTestId } = render(UpdateBanner);
    const banner = await findByTestId("update-banner");
    expect(banner).toBeInTheDocument();
    expect(banner).toHaveTextContent("Ember v0.9.5 is available");
    const link = await findByTestId("update-banner-link");
    expect(link).toHaveAttribute("href", "https://github.com/brandonhon/ember/releases/tag/v0.9.5");
  });

  it("dismiss hides the banner and remembers the version", async () => {
    updateInfo.set(info());
    const { findByTestId, queryByTestId } = render(UpdateBanner);
    await fireEvent.click(await findByTestId("update-banner-dismiss"));
    expect(queryByTestId("update-banner")).toBeNull();
    expect(localStorage.getItem("ember:update-dismissed")).toBe("v0.9.5");
  });

  it("stays dismissed for the same version on a fresh mount", () => {
    localStorage.setItem("ember:update-dismissed", "v0.9.5");
    updateInfo.set(info());
    const { queryByTestId } = render(UpdateBanner);
    expect(queryByTestId("update-banner")).toBeNull();
  });

  it("re-shows when a newer version arrives after a prior dismissal", async () => {
    localStorage.setItem("ember:update-dismissed", "v0.9.5");
    updateInfo.set(info({ latest: "v0.9.6", url: "https://example.test/v0.9.6" }));
    const { findByTestId } = render(UpdateBanner);
    const banner = await findByTestId("update-banner");
    expect(banner).toHaveTextContent("Ember v0.9.6 is available");
  });
});
