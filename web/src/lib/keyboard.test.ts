import { describe, it, expect } from "vitest";
import { shortcutFor } from "./keyboard";

describe("shortcutFor", () => {
  it("maps j/k to next/prev", () => {
    expect(shortcutFor({ key: "j" })).toBe("next");
    expect(shortcutFor({ key: "k" })).toBe("prev");
  });

  it("maps action keys", () => {
    expect(shortcutFor({ key: "o" })).toBe("open-original");
    expect(shortcutFor({ key: "m" })).toBe("toggle-read");
    expect(shortcutFor({ key: "s" })).toBe("toggle-star");
    expect(shortcutFor({ key: "r" })).toBe("refresh");
    expect(shortcutFor({ key: "/" })).toBe("focus-search");
    expect(shortcutFor({ key: "?" })).toBe("show-help");
  });

  it("returns null for unknown keys", () => {
    expect(shortcutFor({ key: "x" })).toBeNull();
    expect(shortcutFor({ key: "Enter" })).toBeNull();
  });

  it("ignores keys when modifier is held", () => {
    expect(shortcutFor({ key: "j", ctrlKey: true })).toBeNull();
    expect(shortcutFor({ key: "j", metaKey: true })).toBeNull();
    expect(shortcutFor({ key: "j", altKey: true })).toBeNull();
  });

  it("ignores keys when focus is in a form field", () => {
    expect(shortcutFor({ key: "j", target: { tagName: "INPUT" } })).toBeNull();
    expect(shortcutFor({ key: "j", target: { tagName: "TEXTAREA" } })).toBeNull();
  });

  it("still works when focus is on a div", () => {
    expect(shortcutFor({ key: "j", target: { tagName: "DIV" } })).toBe("next");
  });
});
