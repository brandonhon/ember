import { render, screen } from "@testing-library/svelte";
import { describe, it, expect } from "vitest";
import App from "./App.svelte";

describe("App smoke test", () => {
  it("mounts and renders the title", () => {
    render(App);
    const title = screen.getByTestId("app-title");
    expect(title).toBeInTheDocument();
    expect(title).toHaveTextContent("Ember");
  });
});
