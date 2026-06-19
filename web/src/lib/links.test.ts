import { describe, it, expect } from "vitest";
import { forceNewTabLinks } from "./links";

describe("forceNewTabLinks", () => {
  it("adds target=_blank and rel=noopener noreferrer to every href link", () => {
    const el = document.createElement("div");
    el.innerHTML = `<p><a href="https://a.example">a</a> and <a href="/relative">b</a></p>`;
    forceNewTabLinks(el);
    const links = el.querySelectorAll("a");
    expect(links).toHaveLength(2);
    links.forEach((a) => {
      expect(a.getAttribute("target")).toBe("_blank");
      expect(a.getAttribute("rel")).toBe("noopener noreferrer");
    });
  });

  it("overrides an existing target/rel so it can't be a tab-napping vector", () => {
    const el = document.createElement("div");
    el.innerHTML = `<a href="https://x.example" target="_self" rel="nofollow">x</a>`;
    forceNewTabLinks(el);
    const a = el.querySelector("a")!;
    expect(a.getAttribute("target")).toBe("_blank");
    expect(a.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("leaves anchors without an href untouched", () => {
    const el = document.createElement("div");
    el.innerHTML = `<a id="anchor">no href</a>`;
    forceNewTabLinks(el);
    const a = el.querySelector("a")!;
    expect(a.hasAttribute("target")).toBe(false);
    expect(a.hasAttribute("rel")).toBe(false);
  });
});
