import "@testing-library/jest-dom/vitest";
import { vi } from "vitest";

// jsdom doesn't implement window.matchMedia; the SPA's theme code calls it
// during mount to read prefers-color-scheme. Stub with a never-matches
// MediaQueryList so the boot path (App.svelte) renders without throwing.
if (typeof window !== "undefined" && !window.matchMedia) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(() => false),
    })),
  });
}
