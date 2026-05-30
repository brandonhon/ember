import { writable, derived, get } from "svelte/store";
import type {
  ArticleView,
  Board,
  Category,
  FeedWithCounts,
  ListArticlesQuery,
  User,
} from "./types";
import { api, ApiError } from "./api";

// Auth ----------------------------------------------------------------------
export const user = writable<User | null>(null);
export const feverAPIKey = writable<string>("");
export const appVersion = writable<string>("");
// Server-configured Fresh-view cutoff in seconds (EMBER_FRESH_WINDOW).
// Used by ArticleList.svelte's isFresh() so the client filter matches the
// server's CountSmartViews query. 6h default until /api/me resolves.
export const freshWindowSeconds = writable<number>(6 * 3600);

export async function refreshMe(): Promise<User | null> {
  try {
    const res = await api.me();
    user.set(res.data.user);
    feverAPIKey.set(res.data.fever_api_key);
    appVersion.set(res.data.version);
    if (res.data.fresh_window_seconds && res.data.fresh_window_seconds > 0) {
      freshWindowSeconds.set(res.data.fresh_window_seconds);
    }
    return res.data.user;
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      user.set(null);
      return null;
    }
    throw err;
  }
}

export async function login(username: string, password: string): Promise<void> {
  const res = await api.login(username, password);
  user.set(res.data);
}

export async function logout(): Promise<void> {
  try {
    await api.logout();
  } finally {
    user.set(null);
  }
}

// Feeds / categories / boards -----------------------------------------------
export const feeds = writable<FeedWithCounts[]>([]);
export const categories = writable<Category[]>([]);
export const boards = writable<Board[]>([]);
export const savedSearches = writable<import("./types").SavedSearch[]>([]);

// Smart-view badge counts (Fresh / Starred / Read Later / Shared) plus the
// summarizer pending-queue count. Refreshed alongside the sidebar so the
// badges + summarizing indicator stay live.
export interface SmartCounts {
  fresh: number;
  starred: number;
  later: number;
  shared: number;
  pending_summary: number;
}
const EMPTY_SMART_COUNTS: SmartCounts = { fresh: 0, starred: 0, later: 0, shared: 0, pending_summary: 0 };
export const smartCounts = writable<SmartCounts>(EMPTY_SMART_COUNTS);

export async function refreshSidebar(): Promise<void> {
  const [f, c, b, ss, sc] = await Promise.all([
    api.listFeeds(),
    api.listCategories(),
    api.listBoards(),
    api.listSavedSearches(),
    api.getSmartCounts(),
  ]);
  feeds.set(f.data ?? []);
  categories.set(c.data ?? []);
  boards.set(b.data ?? []);
  savedSearches.set(ss.data ?? []);
  smartCounts.set(sc.data ?? EMPTY_SMART_COUNTS);
}

// refreshSmartCounts refreshes only the smart-view badge counts (incl.
// pending_summary). Cheaper than refreshSidebar and used by the poll loop to
// drive the "Summarizing N…" indicator down to zero while the summary worker
// chews through a backlog — otherwise the count only refreshes when new
// articles happen to arrive, leaving the bar stuck after summarization
// finishes.
export async function refreshSmartCounts(): Promise<void> {
  const sc = await api.getSmartCounts();
  smartCounts.set(sc.data ?? EMPTY_SMART_COUNTS);
}

export const totalUnread = derived(feeds, ($feeds) =>
  $feeds.reduce((n, f) => n + (f.unread || 0), 0),
);

// View / UI state ------------------------------------------------------------
export type ActiveView =
  | { kind: "smart"; view: "fresh" | "today" | "unread" | "starred" | "later" | "shared" }
  | { kind: "feed"; id: number }
  | { kind: "category"; id: number }
  | { kind: "board"; id: number }
  | { kind: "search"; query: string; savedID?: number };

export const activeView = writable<ActiveView>({ kind: "smart", view: "fresh" });
export const selectedArticleId = writable<number | null>(null);
// Hydrate from localStorage so theme + density persist across reloads. Guard
// for non-browser test environments (jsdom may define localStorage as a stub).
function loadPref<T extends string>(key: string, fallback: T): T {
  try {
    const v = globalThis.localStorage?.getItem(key);
    return (v as T) || fallback;
  } catch {
    return fallback;
  }
}
// Theme: "auto" follows the OS prefers-color-scheme; the rest are explicit
// presets. The DOM data-theme attribute always carries a concrete palette
// (App.svelte resolves "auto" → "light"/"dark" via matchMedia).
export type Theme = "auto" | "light" | "dark" | "solarized" | "sepia" | "nord" | "gruvbox" | "contrast" | "custom";
export const THEMES: { value: Theme; label: string; mood: "light" | "dark" }[] = [
  { value: "auto", label: "Auto (OS)", mood: "light" },
  { value: "light", label: "Light", mood: "light" },
  { value: "dark", label: "Dark", mood: "dark" },
  { value: "solarized", label: "Solarized", mood: "light" },
  { value: "sepia", label: "Sepia", mood: "light" },
  { value: "nord", label: "Nord", mood: "dark" },
  { value: "gruvbox", label: "Gruvbox", mood: "dark" },
  { value: "contrast", label: "High contrast", mood: "dark" },
  { value: "custom", label: "Custom", mood: "light" },
];
export const theme = writable<Theme>(loadPref<Theme>("ember:theme", "auto"));

// Custom theme palette — user picks paper/ink/ember/link; everything else is
// derived in App.svelte via color-mix(). Stored as JSON for simple persistence.
export interface CustomPalette {
  paper: string;
  ink: string;
  ember: string;
  // Anchor color override for the custom theme. Forward-compatible: old
  // localStorage entries that omit this fall through to DEFAULT_CUSTOM.link
  // via the spread in loadCustom().
  link: string;
}
const DEFAULT_CUSTOM: CustomPalette = { paper: "#f6f2e9", ink: "#211d18", ember: "#a93b16", link: "#a93b16" };
function loadCustom(): CustomPalette {
  try {
    const raw = globalThis.localStorage?.getItem("ember:custom");
    if (!raw) return DEFAULT_CUSTOM;
    const parsed = JSON.parse(raw) as Partial<CustomPalette>;
    return { ...DEFAULT_CUSTOM, ...parsed };
  } catch {
    return DEFAULT_CUSTOM;
  }
}
export const customPalette = writable<CustomPalette>(loadCustom());
customPalette.subscribe((p) => {
  try {
    globalThis.localStorage?.setItem("ember:custom", JSON.stringify(p));
  } catch {
    /* ignore */
  }
});

// App branding (server-wide). Loaded from /api/branding at boot; admins can
// edit via Settings → Branding. Falls back to "Ember" if the endpoint is
// unreachable.
export interface Branding {
  name: string;
  page_title: string;
  favicon_url: string;
}
const DEFAULT_BRANDING: Branding = { name: "Ember", page_title: "Ember", favicon_url: "/icon.svg" };
export const branding = writable<Branding>(DEFAULT_BRANDING);
export async function refreshBranding(): Promise<void> {
  try {
    const res = await fetch("/api/branding", { credentials: "include" });
    if (!res.ok) return;
    const body = (await res.json()) as { data: Partial<Branding> };
    const next = { ...DEFAULT_BRANDING, ...body.data };
    branding.set(next);
    if (typeof document !== "undefined") {
      document.title = next.page_title || next.name || "Ember";
      const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
      if (link && next.favicon_url) link.href = next.favicon_url;
    }
  } catch {
    /* keep defaults */
  }
}
export const density = writable<"card" | "compact">(loadPref("ember:density", "card"));
export const sidebarCollapsed = writable<boolean>(loadPref<string>("ember:sidebar", "open") === "closed");

// Display preferences: AI summary card on/off, hero/inline images on/off,
// and summary-card collapsed state. Persisted in localStorage so the choice
// survives reloads. The server-side EMBER_DISABLE_SUMMARIES flag short-
// circuits summary generation in the poller; these UI prefs just hide the
// already-stored value for the current user.
export const showSummary = writable<boolean>(loadPref<string>("ember:show-summary", "on") !== "off");
export const summaryCollapsed = writable<boolean>(loadPref<string>("ember:summary-collapsed", "open") === "closed");
function persistBool(key: string, store: { subscribe: (cb: (v: boolean) => void) => () => void }, on: string, off: string) {
  store.subscribe((v) => {
    try {
      globalThis.localStorage?.setItem(key, v ? on : off);
    } catch {
      /* ignore */
    }
  });
}
persistBool("ember:show-summary", showSummary, "on", "off");
persistBool("ember:summary-collapsed", summaryCollapsed, "closed", "open");

// Articles list --------------------------------------------------------------
export interface ArticleListState {
  items: ArticleView[];
  loading: boolean;
  cursor?: { pub: number; id: number };
  err?: string;
}

export const articles = writable<ArticleListState>({ items: [], loading: false });

function queryForView(view: ActiveView): ListArticlesQuery {
  switch (view.kind) {
    case "smart":
      return { view: view.view };
    case "feed":
      return { feed_id: view.id };
    case "category":
      return { category_id: view.id };
    case "board":
      return { board_id: view.id };
    case "search":
      // Search uses a different endpoint; loadArticles handles it specially.
      return {};
  }
}

// newArticleCount tracks unseen articles that have arrived since the user
// last sat at the top of the list. Drives the green favicon-dot indicator
// in App.svelte. Reset to 0 when the user is at the top of the list AND the
// tab is visible (App.svelte handles that).
export const newArticleCount = writable<number>(0);

// pollForNewArticles fetches the current view's top page and merges any
// articles whose id is higher than the current top into the store. Runs
// every 30s while the tab is visible; the user never has to refresh.
export async function pollForNewArticles(): Promise<number> {
  const view = get(activeView);
  // Search is a one-shot FTS lookup, not a stream — auto-refresh is
  // meaningless here and would clobber the user's results.
  if (view.kind === "search") return 0;
  const current = get(articles);
  if (current.loading) return 0;
  try {
    const q = queryForView(view);
    const res = await api.listArticles({ ...q });
    const fresh = res.data ?? [];
    if (fresh.length === 0 && current.items.length === 0) return 0;

    // Merge semantics: the server-returned top page (`fresh`) is authoritative
    // for any article whose id it contains (so read/star/dedup state flows
    // through). Existing items not in `fresh` are preserved at their natural
    // sort position — this keeps the user's scrolled-down position AND keeps
    // a currently-selected reader-pane article from disappearing when the
    // poll drops it from the top page (or the smart-view filter excludes it).
    // Sort matches the server's ORDER BY IFNULL(published_at,0) DESC, id DESC.
    const have = new Set(current.items.map((a) => a.id));
    const newCount = fresh.reduce((n, a) => (have.has(a.id) ? n : n + 1), 0);
    const freshIds = new Set(fresh.map((a) => a.id));
    const kept = current.items.filter((a) => !freshIds.has(a.id));
    const merged = [...fresh, ...kept].sort((a, b) => {
      const aPub = a.published_at ?? 0;
      const bPub = b.published_at ?? 0;
      if (bPub !== aPub) return bPub - aPub;
      return b.id - a.id;
    });
    articles.update((s) => ({ ...s, items: merged, loading: false, err: undefined }));
    if (newCount > 0) {
      newArticleCount.update((n) => n + newCount);
      void refreshSidebar();
    }
    return newCount;
  } catch {
    return 0;
  }
}

export async function loadArticles(view: ActiveView, append = false): Promise<void> {
  articles.update((s) => ({ ...s, loading: true, err: undefined }));
  // Search view: hit /api/search and treat the results as the article list.
  // FTS is not paginated, so append is ignored.
  if (view.kind === "search") {
    try {
      const res = await api.search(view.query, 100);
      articles.update(() => ({
        items: (res.data ?? []) as ArticleView[],
        loading: false,
      }));
      newArticleCount.set(0);
    } catch (err) {
      articles.update((s) => ({ ...s, loading: false, err: String(err) }));
    }
    return;
  }
  try {
    const q = queryForView(view);
    if (append) {
      const cur = get(articles).cursor;
      if (cur) {
        q.cursor_pub = cur.pub;
        q.cursor_id = cur.id;
      }
    }
    const res = await api.listArticles(q);
    const meta = res.meta ?? {};
    const next = res.data?.length
      ? { pub: Number(meta.next_cursor_pub ?? 0), id: Number(meta.next_cursor_id ?? 0) }
      : undefined;
    articles.update((s) => ({
      items: append ? [...s.items, ...(res.data ?? [])] : res.data ?? [],
      loading: false,
      cursor: next,
    }));
    if (!append) {
      // Switching views resets the "new" indicator — the user is looking at
      // the fresh top of the new view.
      newArticleCount.set(0);
    }
  } catch (err) {
    articles.update((s) => ({ ...s, loading: false, err: String(err) }));
  }
}

// Read/star toggles update the local list optimistically so the UI feels snappy.
export async function setRead(ids: number[], read: boolean): Promise<void> {
  // Capture which items were fresh+unread BEFORE the optimistic flip so we can
  // bump smartCounts.fresh by the right delta. Fresh-eligibility is computed
  // the same way ArticleList.isFresh() does it — client-side from
  // published_at + freshWindowSeconds — so the badge tracks the visible list.
  const idSet = new Set(ids);
  const nowSec = Date.now() / 1000;
  const windowSec = get(freshWindowSeconds);
  const freshDelta = get(articles).items.reduce((n, a) => {
    if (!idSet.has(a.id)) return n;
    if (!a.published_at) return n;
    if (nowSec - a.published_at >= windowSec) return n;
    // Only count items whose is_read state is actually changing.
    if (!!a.is_read === read) return n;
    return n + 1;
  }, 0);

  articles.update((s) => ({
    ...s,
    items: s.items.map((a) => (ids.includes(a.id) ? { ...a, is_read: read } : a)),
  }));
  if (freshDelta !== 0) {
    smartCounts.update((c) => ({
      ...c,
      fresh: Math.max(0, c.fresh + (read ? -freshDelta : freshDelta)),
    }));
  }
  try {
    await api.setRead(ids, read);
  } catch (err) {
    // Roll back the fresh-count bump; the feed-unread update below never ran.
    if (freshDelta !== 0) {
      smartCounts.update((c) => ({
        ...c,
        fresh: Math.max(0, c.fresh + (read ? freshDelta : -freshDelta)),
      }));
    }
    throw err;
  }
  feeds.update((fs) =>
    fs.map((f) => {
      const delta = ids.filter((id) => {
        const item = get(articles).items.find((a) => a.id === id);
        return item?.feed_id === f.id;
      }).length;
      if (delta === 0) return f;
      return { ...f, unread: Math.max(0, f.unread + (read ? -delta : delta)) };
    }),
  );
}

// toggleStar / toggleLater do two optimistic updates so the UI feels
// immediate:
//   1. flip the article's flag in the loaded list (already existed).
//   2. bump the sidebar's smart-count badge by ±1 (NEW — fixes the
//      reported "I have to refresh to see the count update").
// On API failure, both updates roll back to the captured prior state.
// We compute the delta from the article's PREVIOUS flag so a no-op
// toggle (same value as current) bumps by 0.

export async function toggleStar(id: number, value: boolean): Promise<void> {
  const prev = get(articles).items.find((a) => a.id === id);
  const delta = prev ? (value === !!prev.is_starred ? 0 : value ? 1 : -1) : 0;
  articles.update((s) => ({
    ...s,
    items: s.items.map((a) => (a.id === id ? { ...a, is_starred: value } : a)),
  }));
  if (delta !== 0) {
    smartCounts.update((c) => ({ ...c, starred: Math.max(0, c.starred + delta) }));
  }
  try {
    await api.setStar(id, value);
  } catch (err) {
    // Roll back both optimistic updates.
    articles.update((s) => ({
      ...s,
      items: s.items.map((a) => (a.id === id ? { ...a, is_starred: !!prev?.is_starred } : a)),
    }));
    if (delta !== 0) {
      smartCounts.update((c) => ({ ...c, starred: Math.max(0, c.starred - delta) }));
    }
    throw err;
  }
}

export async function toggleLater(id: number, value: boolean): Promise<void> {
  const prev = get(articles).items.find((a) => a.id === id);
  const delta = prev ? (value === !!prev.is_later ? 0 : value ? 1 : -1) : 0;
  articles.update((s) => ({
    ...s,
    items: s.items.map((a) => (a.id === id ? { ...a, is_later: value } : a)),
  }));
  if (delta !== 0) {
    smartCounts.update((c) => ({ ...c, later: Math.max(0, c.later + delta) }));
  }
  try {
    await api.setLater(id, value);
  } catch (err) {
    articles.update((s) => ({
      ...s,
      items: s.items.map((a) => (a.id === id ? { ...a, is_later: !!prev?.is_later } : a)),
    }));
    if (delta !== 0) {
      smartCounts.update((c) => ({ ...c, later: Math.max(0, c.later - delta) }));
    }
    throw err;
  }
}
