import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

const isTest = !!process.env.VITEST;

export default defineConfig({
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
