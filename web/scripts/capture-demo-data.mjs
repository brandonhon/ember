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
  // demo-data.json is imported as an ES module, so every byte here lands in the
  // bundle the demo page parses before first render. A handful of real articles
  // are pathologically large — a CISA advisory enumerating hundreds of CVEs came
  // in at 1.0 MB and an SBCL changelog at 773 KB, together 87% of all content
  // across 50 articles (median: 2 KB). They also make poor demo cards. Skip them
  // at selection time rather than truncating everything: the other 50 stay whole.
  const MAX_HTML = Number(process.env.DEMO_MAX_ARTICLE_BYTES || 64 * 1024);
  const summarized = raw.filter((a) => {
    if (!a.summary || !a.summary.trim()) return false;
    if ((a.content_html || "").length > MAX_HTML) {
      console.log(`  skipping oversized article (${Math.round((a.content_html || "").length / 1024)} KB): ${a.title.slice(0, 60)}`);
      return false;
    }
    return true;
  });
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

  // Un-proxy lead images. Since v0.9.3 the API rewrites `image_url` to a
  // same-origin `/api/img?u=<src>&s=<hmac>` path (article_handlers.go,
  // search_handlers.go). That signature is verified against a key derived from
  // this server's session key, so in the static demo — which has no server —
  // every one of those paths is a dead link. `<img src>` also never reaches the
  // fetch shim, so this cannot be fixed in demo.ts; it has to be fixed in the
  // captured data. The original URL round-trips out of the `u` parameter.
  //
  // Trade-off worth knowing: direct publisher-CDN URLs are what the proxy
  // exists to avoid, because content blockers match on those domains and strip
  // the images. A static demo has no origin to serve them from, so it accepts
  // that; the alternative (inlining every image as a data: URI) would add
  // megabytes to a file the page loads up front.
  // content_text is never rendered — the reader uses cleaned_html || content_html.
  // Its only consumer is demo.ts's search, which substring-matches it. Keeping
  // the lede is enough for that and halves the payload.
  const TEXT_CAP = 1200;
  for (const a of picked) {
    if (typeof a.content_text === "string" && a.content_text.length > TEXT_CAP) {
      a.content_text = a.content_text.slice(0, TEXT_CAP);
    }
  }

  let unproxied = 0;
  for (const a of picked) {
    if (typeof a.image_url === "string" && a.image_url.startsWith("/api/img?")) {
      const src = new URLSearchParams(a.image_url.slice("/api/img?".length)).get("u");
      if (src) { a.image_url = src; unproxied++; }
      else delete a.image_url;
    }
  }
  console.log(`un-proxied ${unproxied}/${picked.length} lead images`);

  // Drop lead images that don't actually resolve. Feeds ship broken ones — NPR
  // emitted a literal `https://feeds.npr.org/1004/undefined` — and while the
  // card's on:error handler hides them, a frozen demo would re-request every
  // known-dead URL on every page view. Checked here, once, instead.
  const checks = await Promise.all(
    picked.map(async (a) => {
      if (!a.image_url || !/^https?:\/\//.test(a.image_url)) return true;
      try {
        const r = await fetch(a.image_url, { method: "GET", headers: { Range: "bytes=0-0" }, redirect: "follow" });
        return r.ok || r.status === 206;
      } catch { return false; }
    }),
  );
  let dropped = 0;
  picked.forEach((a, i) => { if (!checks[i]) { delete a.image_url; dropped++; } });
  console.log(`dropped ${dropped} unreachable lead images`);

  // Pre-populate a few starred / later so those smart views aren't empty.
  picked.forEach((a, i) => {
    a.is_read = false;
    a.is_starred = i % 11 === 3;     // ~5 starred
    a.is_later = i % 13 === 5;       // ~4 later
  });

  // Public demo identity (mask the real admin account; regular-user view so
  // admin-only panels stay hidden and nothing tries a broken admin GET).
  // No fresh_window_seconds / unread_window_seconds here on purpose. The real
  // instance reports a 6h window, and refreshMe() writes whatever /api/me
  // returns straight into the window stores — which would immediately narrow
  // the frozen data out of every wall-clock check (empty "Fresh only", no
  // "Fresh" tags). demo.ts owns those values (DEMO_WINDOWS) so there is exactly
  // one place that defines them; capturing them here would be a second one.
  const demoMe = {
    user: { id: 1, username: "demo", is_admin: false, settings_json: "{}", created_at: me.user?.created_at ?? 0 },
    fever_api_key: "demo-fever-key",
    // Literal "demo", never the capture host's version: pages.yml passes the
    // real release tag as VITE_DEMO_VERSION, and demo.ts only falls back to
    // this for local builds. Capturing me.version would bake a dev string
    // ("v0.9.5-20-g7f35e10") into a committed, publicly served artifact.
    version: "demo",
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
