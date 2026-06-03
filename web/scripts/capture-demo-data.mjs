// Capture a running ember instance's API into web/src/demo/demo-data.json for
// the static GitHub Pages demo. Run against a real, seeded, summarized stack:
//
//   EMBER_API=https://localhost:8443 EMBER_USER=admin EMBER_PASS=... \
//     node web/scripts/capture-demo-data.mjs
//
// It logs in, pulls the reference data + ~50 summarized articles, normalizes
// the user to the public "demo" identity, pre-stars/later a few for non-empty
// smart views, and writes the JSON the demo backend (web/src/demo/demo.ts)
// hydrates from. Self-signed TLS is accepted (local Caddy).

import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0"; // local self-signed cert

const API = process.env.EMBER_API || "https://localhost:8443";
const USER = process.env.EMBER_USER || "admin";
const PASS = process.env.EMBER_PASS || "";
const WANT = Number(process.env.DEMO_ARTICLES || 50);

const here = dirname(fileURLToPath(import.meta.url));
const OUT = resolve(here, "../src/demo/demo-data.json");

// Cookie jar keyed by name, LAST value wins. The login response sends two
// `ember_session` Set-Cookie headers (an empty clear, then the real value);
// a naive concat sends both and the server reads the empty one → 401.
const jar = new Map();
let csrf = "";

function absorb(res) {
  for (const c of res.headers.getSetCookie?.() ?? []) {
    const pair = c.split(";")[0];
    const eq = pair.indexOf("=");
    if (eq < 0) continue;
    const name = pair.slice(0, eq);
    const val = pair.slice(eq + 1);
    jar.set(name, val);
    if (name === "ember_csrf") csrf = decodeURIComponent(val);
  }
}
function cookieHeader() {
  return [...jar.entries()].filter(([, v]) => v).map(([k, v]) => `${k}=${v}`).join("; ");
}

async function req(method, path, body) {
  const headers = { "Content-Type": "application/json" };
  const ch = cookieHeader();
  if (ch) headers["Cookie"] = ch;
  if (csrf && method !== "GET") headers["X-Ember-CSRF"] = csrf;
  const res = await fetch(API + path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });
  absorb(res);
  if (!res.ok) throw new Error(`${method} ${path} -> ${res.status}`);
  return (await res.json()).data;
}

async function main() {
  await req("POST", "/api/auth/login", { username: USER, password: PASS });
  console.log("logged in as", USER);

  const [me, branding, categories, feeds, boards, filters, savedSearches, tags, starterPacks, stats] =
    await Promise.all([
      req("GET", "/api/me"),
      req("GET", "/api/branding"),
      req("GET", "/api/categories"),
      req("GET", "/api/feeds"),
      req("GET", "/api/boards"),
      req("GET", "/api/filters"),
      req("GET", "/api/saved-searches"),
      req("GET", "/api/tags"),
      req("GET", "/api/starter-packs"),
      req("GET", "/api/me/stats"),
    ]);

  // Pull a wide page of unread, keep only summarized ones, take WANT of them
  // with a round-robin spread across feeds so no single feed dominates.
  const raw = await req("GET", "/api/articles?view=unread&limit=300");
  const summarized = raw.filter((a) => a.summary && a.summary.trim());
  const byFeed = new Map();
  for (const a of summarized) {
    if (!byFeed.has(a.feed_id)) byFeed.set(a.feed_id, []);
    byFeed.get(a.feed_id).push(a);
  }
  const picked = [];
  let added = true;
  while (picked.length < WANT && added) {
    added = false;
    for (const list of byFeed.values()) {
      if (list.length && picked.length < WANT) { picked.push(list.shift()); added = true; }
    }
  }
  // Newest first.
  picked.sort((a, b) => (b.published_at ?? 0) - (a.published_at ?? 0) || b.id - a.id);

  // Pre-populate a few starred / later so those smart views aren't empty.
  picked.forEach((a, i) => {
    a.is_read = false;
    a.is_starred = i % 11 === 3;     // ~5 starred
    a.is_later = i % 13 === 5;       // ~4 later
  });

  // Public demo identity (mask the real admin account; regular-user view so
  // admin-only panels stay hidden and nothing tries a broken admin GET).
  const demoMe = {
    user: { id: 1, username: "demo", is_admin: false, settings_json: "{}", created_at: me.user?.created_at ?? 0 },
    fever_api_key: "demo-fever-key",
    version: me.version || "demo",
    fresh_window_seconds: me.fresh_window_seconds || 21600,
  };

  const today = new Date().toISOString().slice(0, 10);
  const out = {
    captured_at: today,
    me: demoMe,
    // Empty favicon_url → the SPA falls back to its base-aware bundled icon
    // (import.meta.env.BASE_URL + icon.svg), which resolves under /ember/demo/.
    branding: { name: branding.name || "Ember", page_title: branding.page_title || "Ember", favicon_url: "" },
    categories,
    feeds,
    boards,
    filters,
    savedSearches,
    tags,
    starterPacks,
    stats,
    articles: picked,
  };

  writeFileSync(OUT, JSON.stringify(out, null, 2) + "\n");
  console.log(`wrote ${OUT}`);
  console.log(`  feeds=${feeds.length} categories=${categories.length} articles=${picked.length} (of ${summarized.length} summarized)`);
  console.log(`  starred=${picked.filter((a) => a.is_starred).length} later=${picked.filter((a) => a.is_later).length}`);
}

main().catch((e) => { console.error(e); process.exit(1); });
