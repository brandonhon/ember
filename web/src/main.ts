import { mount } from "svelte";
import App from "./App.svelte";
import { DEMO, installDemo } from "./demo/demo";

// Demo mode: install the in-memory /api shim BEFORE App mounts (App.onMount
// immediately fetches /api/me + /api/branding). No-op outside demo builds.
installDemo();

const target = document.getElementById("app");
if (!target) {
  throw new Error("missing #app root");
}

// Register the service worker, but only over HTTPS or localhost. We skip it
// entirely during dev (vite dev server) to avoid caching stale dev bundles,
// and in demo builds (no backend, served under a subpath where /sw.js 404s).
if (
  !DEMO &&
  "serviceWorker" in navigator &&
  (location.protocol === "https:" || location.hostname === "localhost" || location.hostname === "127.0.0.1") &&
  !location.port.endsWith("5173") // skip vite dev
) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => {
      /* ignore — PWA is progressive */
    });
  });
}

export default mount(App, { target });
