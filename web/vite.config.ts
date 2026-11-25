import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

const isTest = !!process.env.VITEST;

// Demo build knobs (set by the `build:demo` script / pages.yml):
//   VITE_DEMO_MODE=1          → ship the frozen, mocked-API demo
//   VITE_DEMO_BASE=/demo/     → subpath under the docs site (ember-rss.com/demo/)
// Injected via `define` so they survive a plain `vite build` from the shell
// (Vite only auto-loads VITE_* from .env files, not the process env).
const isDemo = process.env.VITE_DEMO_MODE === "1";

export default defineConfig({
  base: process.env.VITE_DEMO_BASE || "/",
  define: {
    "import.meta.env.VITE_DEMO_MODE": JSON.stringify(process.env.VITE_DEMO_MODE ?? ""),
    "import.meta.env.VITE_DEMO_DATE": JSON.stringify(process.env.VITE_DEMO_DATE ?? ""),
  },
  plugins: [
    svelte({ hot: !isTest }),
    // The public demo (ember-rss.com/demo/) renders nothing until its JS runs,
    // so keep it out of search results. noindex rather than a robots.txt
    // Disallow: a crawler has to fetch the page to see noindex, while a
    // Disallowed URL can still be indexed from links. Demo build only.
    isDemo && {
      name: "demo-noindex",
      transformIndexHtml: () => [
        { tag: "meta", attrs: { name: "robots", content: "noindex, follow" }, injectTo: "head" as const },
      ],
    },
  ],
  resolve: isTest ? { conditions: ["browser"] } : undefined,
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://localhost:8080",
      "/fever": "http://localhost:8080",
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    include: ["src/**/*.{test,spec}.{ts,svelte}"],
    setupFiles: ["./src/test-setup.ts"],
  },
});
