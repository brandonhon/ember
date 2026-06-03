import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

const isTest = !!process.env.VITEST;

// Demo build knobs (set by the `build:demo` script / pages.yml):
//   VITE_DEMO_MODE=1          → ship the frozen, mocked-API demo
//   VITE_DEMO_BASE=/ember/demo/ → subpath for GitHub Pages
// Injected via `define` so they survive a plain `vite build` from the shell
// (Vite only auto-loads VITE_* from .env files, not the process env).
export default defineConfig({
  base: process.env.VITE_DEMO_BASE || "/",
  define: {
    "import.meta.env.VITE_DEMO_MODE": JSON.stringify(process.env.VITE_DEMO_MODE ?? ""),
    "import.meta.env.VITE_DEMO_DATE": JSON.stringify(process.env.VITE_DEMO_DATE ?? ""),
  },
  plugins: [svelte({ hot: !isTest })],
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
