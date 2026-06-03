// Ember service worker.
// Strategy:
//   - Precache the SPA shell on install (index.html).
//   - Runtime: cache-first for /assets/* (hashed, immutable) and other same-
//     origin static files.
//   - Network-first for /api/* and /fever, falling back to the cache only on
//     network failure (so users always get fresh state when online but the
//     reading UI still works offline for the last-seen state).
//   - Auth endpoints and CSRF-sensitive POSTs are never cached.

// Bumped 2026-05-26 to flush stale shells that still served the old
// inline "Welcome to Ember" onboarding card (replaced by WelcomeModal in
// PR #32) and the old favicon defaults (PR #33). Old SHELL_CACHE entries
// get deleted on activate.
const VERSION = "ember-v2";
const SHELL_CACHE = `${VERSION}-shell`;
const ASSET_CACHE = `${VERSION}-assets`;
const API_CACHE = `${VERSION}-api`;

const SHELL = ["/", "/manifest.webmanifest", "/icon.svg"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(SHELL_CACHE);
      await cache.addAll(SHELL);
      await self.skipWaiting();
    })(),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys();
      await Promise.all(
        keys
          .filter((k) => !k.startsWith(VERSION))
          .map((k) => caches.delete(k)),
      );
      await self.clients.claim();
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  const url = new URL(req.url);

  // Only handle same-origin GETs.
  if (req.method !== "GET" || url.origin !== self.location.origin) return;

  if (url.pathname.startsWith("/assets/")) {
    event.respondWith(cacheFirst(req, ASSET_CACHE));
    return;
  }
  // Auth + CSRF-sensitive endpoints — never cache.
  if (
    url.pathname.startsWith("/api/auth/") ||
    url.pathname.startsWith("/fever") ||
    url.pathname === "/api/me"
  ) {
    return;
  }
  if (url.pathname.startsWith("/api/articles") || url.pathname.startsWith("/api/feeds")) {
    event.respondWith(networkFirst(req, API_CACHE));
    return;
  }
  // SPA navigation requests fall back to the cached shell when offline.
  if (req.mode === "navigate") {
    event.respondWith(networkFirst(req, SHELL_CACHE));
    return;
  }
});

async function cacheFirst(req, cacheName) {
  const cache = await caches.open(cacheName);
  const cached = await cache.match(req);
  if (cached) return cached;
  const res = await fetch(req);
  if (res.ok) cache.put(req, res.clone());
  return res;
}

async function networkFirst(req, cacheName) {
  const cache = await caches.open(cacheName);
  try {
    const res = await fetch(req);
    if (res.ok) cache.put(req, res.clone());
    return res;
  } catch (err) {
    const cached = await cache.match(req);
    if (cached) return cached;
    throw err;
  }
}
