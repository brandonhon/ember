// Demo mode: a fully client-side, frozen, read-from-JSON backend.
//
// When built with VITE_DEMO_MODE=1 the SPA ships to GitHub Pages with no real
// server. We install a window.fetch shim that intercepts every /api/* request
// and answers it from an in-memory store hydrated from demo-data.json. GET
// requests serve (and filter) seeded data; mutations update the in-memory
// store so reading / starring / search feel live within the session and reset
// on reload. Auth is faked: /api/me returns 401 so the real Login screen shows
// (Login.svelte then auto-fills demo/demo and submits), and /api/auth/login
// always succeeds.
//
// This is the single seam for the whole demo — api.ts and the stores are
// untouched, because everything funnels through fetch().

import { writable } from "svelte/store";
import demoData from "./demo-data.json";
import { freshWindowSeconds } from "../lib/stores";
import type { ArticleView, FeedWithCounts } from "../lib/types";

export const DEMO: boolean = import.meta.env.VITE_DEMO_MODE === "1";

// Version reported by the demo's /api/me (drives Settings → About). Injected at
// build time via VITE_DEMO_VERSION — pages.yml sets it to the latest release tag
// so the demo tracks releases; falls back to the captured value for local builds.
const DEMO_VERSION: string = import.meta.env.VITE_DEMO_VERSION || demoData.me.version;

// Drives the "this is a demo site" modal (DemoNotice.svelte). Fired by the
// shim when a write that can't persist is attempted, and directly by
// components for actions that bypass fetch (e.g. the OPML export navigation).
export const demoNotice = writable(false);
export function notifyDemoBlocked(): void {
  if (DEMO) demoNotice.set(true);
}

// Faked auth: false until the visitor "logs in" so the real Login screen
// shows first; true afterwards so refreshMe() (e.g. Settings close) doesn't
// bounce back to Login.
let loggedIn = false;

// Build-time stamp shown in the banner. Falls back to the captured date.
export const DEMO_DATE: string =
  (import.meta.env.VITE_DEMO_DATE as string) || demoData.captured_at || "";

type Json = Record<string, unknown>;

// Mutable session state — a deep copy so we never scribble on the imported
// JSON module (which Vite may freeze / share).
const state = {
  articles: structuredClone(demoData.articles) as ArticleView[],
  feeds: structuredClone(demoData.feeds) as FeedWithCounts[],
};

// Smart views are computed by flag, NOT by wall-clock vs published_at — the
// demo data is frozen and would otherwise empty out as it ages. "fresh" /
// "today" / "unread" all surface unread items so the landing view is always
// populated; starred / later / shared filter by flag.
function articlesForView(p: URLSearchParams): ArticleView[] {
  const view = p.get("view") || "";
  const feedId = p.get("feed_id");
  const categoryId = p.get("category_id");
  const boardId = p.get("board_id");

  let items = state.articles.slice();
  if (feedId) {
    items = items.filter((a) => a.feed_id === Number(feedId));
  } else if (categoryId) {
    const cid = Number(categoryId);
    const feedIds = new Set(state.feeds.filter((f) => f.category_id === cid).map((f) => f.id));
    items = items.filter((a) => feedIds.has(a.feed_id));
  } else if (boardId) {
    // No board membership in the seed → empty board view.
    items = [];
  } else {
    switch (view) {
      case "starred": items = items.filter((a) => a.is_starred); break;
      case "later":   items = items.filter((a) => a.is_later); break;
      case "shared":  items = []; break;
      case "fresh":
      case "today":
      case "unread":  items = items.filter((a) => !a.is_read); break;
      default: /* "" → everything */ break;
    }
  }
  return items.sort((a, b) => {
    const ap = a.published_at ?? 0;
    const bp = b.published_at ?? 0;
    return bp !== ap ? bp - ap : b.id - a.id;
  });
}

function smartCounts() {
  const unread = state.articles.filter((a) => !a.is_read).length;
  return {
    fresh: unread,
    starred: state.articles.filter((a) => a.is_starred).length,
    later: state.articles.filter((a) => a.is_later).length,
    shared: 0,
    pending_summary: 0,
  };
}

// Feeds carry live unread counts recomputed from the article store so the
// sidebar badges track read/unread toggles.
function feedsWithCounts(): FeedWithCounts[] {
  return state.feeds.map((f) => ({
    ...f,
    unread: state.articles.filter((a) => a.feed_id === f.id && !a.is_read).length,
  }));
}

type RouteResult = { status: number; data?: unknown; meta?: Json };

function route(method: string, path: string, p: URLSearchParams, body: Json | undefined): RouteResult {
  const ok = (data: unknown, meta?: Json): RouteResult => ({ status: 200, data, meta });
  const noContent = (): RouteResult => ({ status: 204 });

  // ---- Auth: show the real login screen, then accept any creds ----
  if (path === "/api/me" && method === "GET") return loggedIn ? ok({ ...demoData.me, version: DEMO_VERSION }) : { status: 401 };
  if (path === "/api/auth/login" && method === "POST") { loggedIn = true; return ok((demoData.me as Json).user); }
  if (path === "/api/auth/logout") { loggedIn = false; return noContent(); }
  if (path === "/api/auth/passkey/exists") return ok({ any_registered: false });
  if (path === "/api/users" && method === "GET") return ok([]);
  if (path === "/api/shares/inbox" && method === "GET") return ok([]);

  // ---- Sidebar / reference data ----
  if (path === "/api/branding") return ok(demoData.branding);
  if (path === "/api/categories" && method === "GET") return ok(demoData.categories);
  if (path === "/api/feeds" && method === "GET") return ok(feedsWithCounts());
  if (path === "/api/boards" && method === "GET") return ok(demoData.boards);
  if (path === "/api/filters" && method === "GET") return ok(demoData.filters);
  if (path === "/api/saved-searches" && method === "GET") return ok(demoData.savedSearches);
  if (path === "/api/tags" && method === "GET") return ok(demoData.tags);
  if (path === "/api/starter-packs" && method === "GET") return ok(demoData.starterPacks);
  if (path === "/api/me/smart-counts") return ok(smartCounts());
  if (path === "/api/me/stats") return ok(demoData.stats);

  // ---- Articles: list / read / mutate ----
  if (path === "/api/articles" && method === "GET") return ok(articlesForView(p));
  const artMatch = path.match(/^\/api\/articles\/(\d+)$/);
  if (artMatch && method === "GET") {
    const a = state.articles.find((x) => x.id === Number(artMatch[1]));
    return a ? ok(a) : { status: 404 };
  }
  if (path.match(/^\/api\/articles\/\d+\/cluster$/)) return ok({ siblings: [] });
  if (path.match(/^\/api\/articles\/\d+\/tags$/) && method === "GET") return ok([]);
  if (path === "/api/articles/read" && method === "POST") {
    const ids = new Set((body?.ids as number[]) || []);
    const read = !!body?.read;
    state.articles.forEach((a) => { if (ids.has(a.id)) a.is_read = read; });
    return ok({ count: ids.size });
  }
  if (path === "/api/articles/star" && method === "POST") {
    const a = state.articles.find((x) => x.id === Number(body?.id));
    if (a) a.is_starred = !!body?.value;
    return noContent();
  }
  if (path === "/api/articles/later" && method === "POST") {
    const a = state.articles.find((x) => x.id === Number(body?.id));
    if (a) a.is_later = !!body?.value;
    return noContent();
  }
  if (path === "/api/articles/mark-all-read" && method === "POST") {
    const view = new URLSearchParams();
    if (body?.feed_id) view.set("feed_id", String(body.feed_id));
    else if (body?.category_id) view.set("category_id", String(body.category_id));
    else if (body?.view) view.set("view", String(body.view));
    const targets = articlesForView(view);
    targets.forEach((t) => { const a = state.articles.find((x) => x.id === t.id); if (a) a.is_read = true; });
    return ok({ count: targets.length });
  }
  if (path.match(/^\/api\/articles\/\d+\/extract$/)) return ok({ status: "no_change" }, { status: "no_change" });

  // ---- Search ----
  if (path === "/api/search" && method === "GET") {
    const q = (p.get("q") || "").toLowerCase();
    if (!q) return ok([]);
    const hits = state.articles
      .filter((a) =>
        a.title.toLowerCase().includes(q) ||
        (a.content_text || "").toLowerCase().includes(q) ||
        (a.summary || "").toLowerCase().includes(q))
      .slice(0, Number(p.get("limit") || 30))
      .map((a, i) => ({ ...a, rank: -i }));
    return ok(hits);
  }

  // ---- Seamless, no-nag interactions: stay quiet so reading feels live.
  //      (Per-article tags, board membership, saved searches, reorder, feed
  //      refresh, filter preview — low-stakes / session-scoped.) ----
  if (path.match(/^\/api\/articles\/\d+\/tags$/)) return ok([]);          // add/remove tag
  if (path.match(/^\/api\/boards\/\d+\/articles/)) return noContent();    // add/remove from board
  if (path === "/api/saved-searches" && method === "POST")
    return ok({ id: Date.now() % 100000, user_id: 1, name: String(body?.name ?? ""), query: String(body?.query ?? ""), created_at: 0 });
  if (path.match(/^\/api\/saved-searches\/\d+$/) && method === "DELETE") return noContent();
  if (path === "/api/categories/reorder" || path === "/api/feeds/reorder") return noContent();
  if (path.match(/^\/api\/feeds\/\d+\/refresh$/)) return noContent();
  if (path === "/api/filters/preview") return ok({ count: 0 });

  // ---- Remaining GETs (admin panels etc.) → benign stub so nothing crashes.
  if (method === "GET") return ok(adminStub(path));

  // ---- Every other write (add feed, OPML import, starter packs, filters,
  //      category/feed management, all settings/admin/branding/digest/
  //      password saves, shares, push, passkeys) can't persist in a static
  //      demo → pop the "this is a demo site" notice. Return an empty object
  //      (200) so callers that read `.data.<field>` get undefined, not a crash. ----
  notifyDemoBlocked();
  return ok({});
}

// Minimal shapes so admin/settings panels render without crashing if opened.
function adminStub(path: string): unknown {
  if (path === "/api/me/digest") return { user_id: 1, enabled: false, view_kind: "smart", view_value: "fresh", hour_utc: 7, minute_utc: 0, last_sent_at: 0, email_override: "" };
  if (path === "/api/me/inbox") return { handle: "", address: "", domain: "", enabled: false };
  if (path === "/api/me/passkeys") return [];
  if (path === "/api/me/push-subscriptions") return [];
  return {};
}

function jsonResponse(r: RouteResult): Response {
  if (r.status === 204) return new Response(null, { status: 204 });
  if (r.status >= 400) {
    return new Response(JSON.stringify({ error: { code: "demo", message: "unavailable in demo" } }), {
      status: r.status,
      headers: { "Content-Type": "application/json" },
    });
  }
  return new Response(JSON.stringify({ data: r.data, meta: r.meta }), {
    status: r.status,
    headers: { "Content-Type": "application/json" },
  });
}

export function installDemo(): void {
  if (!DEMO) return;
  // Widen the Fresh-window so ArticleList's client-side isFresh() keeps frozen
  // (old) articles visible in the default Fresh view forever.
  freshWindowSeconds.set(100 * 365 * 24 * 3600);

  const orig = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    let u: URL;
    try { u = new URL(url, location.origin); } catch { return orig(input, init); }
    if (!u.pathname.startsWith("/api/")) return orig(input, init);

    const method = (init?.method || "GET").toUpperCase();
    let body: Json | undefined;
    if (init?.body && typeof init.body === "string") {
      try { body = JSON.parse(init.body) as Json; } catch { /* form data etc. */ }
    }
    // Tiny latency so optimistic UI + spinners read naturally.
    await new Promise((r) => setTimeout(r, 40));
    return jsonResponse(route(method, u.pathname, u.searchParams, body));
  };
}
