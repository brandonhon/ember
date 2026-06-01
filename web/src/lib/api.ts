// Typed fetch client for the ember API. Throws ApiError on non-2xx.

import type {
  ArticleView,
  Board,
  Category,
  ClusterSibling,
  DiscoveredFeed,
  FeedWithCounts,
  Filter,
  ListArticlesQuery,
  MeResponse,
  PushSubscriptionSummary,
  SavedSearch,
  SearchResult,
  Share,
  User,
} from "./types";

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

type Envelope<T> = { data: T; meta?: Record<string, unknown> };
type ErrorEnvelope = { error: { code: string; message: string } };

function readCSRFCookie(): string | null {
  if (typeof document === "undefined") return null;
  const m = document.cookie.match(/(?:^|;\s*)ember_csrf=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : null;
}

async function call<T>(
  method: string,
  path: string,
  body?: unknown,
  opts?: { signal?: AbortSignal },
): Promise<{ data: T; meta?: Record<string, unknown> }> {
  const init: RequestInit = {
    method,
    credentials: "include",
    signal: opts?.signal,
  };
  const headers: Record<string, string> = {};
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  // CSRF: echo the cookie value as a header for state-changing methods.
  if (method !== "GET" && method !== "HEAD" && method !== "OPTIONS") {
    const tok = readCSRFCookie();
    if (tok) headers["X-Ember-CSRF"] = tok;
  }
  init.headers = headers;
  const res = await fetch(path, init);
  if (res.status === 401 && typeof window !== "undefined" && !path.endsWith("/api/auth/login")) {
    // Centralized 401 handling — clear any UI state via a custom event.
    window.dispatchEvent(new CustomEvent("ember:unauthorized"));
  }
  if (!res.ok) {
    let code = "http_" + res.status;
    let message = res.statusText;
    try {
      const j = (await res.json()) as ErrorEnvelope;
      if (j?.error?.code) code = j.error.code;
      if (j?.error?.message) message = j.error.message;
    } catch {
      /* non-JSON body, keep defaults */
    }
    throw new ApiError(res.status, code, message);
  }
  if (res.status === 204) return { data: undefined as T };
  const j = (await res.json()) as Envelope<T>;
  return { data: j.data, meta: j.meta };
}

export const api = {
  // Auth ---------------------------------------------------------------
  login: (username: string, password: string) =>
    call<User>("POST", "/api/auth/login", { username, password }),
  logout: () => call<unknown>("POST", "/api/auth/logout"),
  me: () => call<MeResponse>("GET", "/api/me"),
  changePassword: (old_password: string, new_password: string) =>
    call<{ ok: boolean }>("POST", "/api/me/password", { old_password, new_password }),
  updateSettings: (settings_json: string) =>
    call<unknown>("PATCH", "/api/me/settings", { settings_json }),

  // Users -------------------------------------------------------------
  listUsers: () => call<User[]>("GET", "/api/users"),
  createUser: (req: { username: string; password: string; email?: string; is_admin?: boolean }) =>
    call<User>("POST", "/api/users", req),
  updateUser: (id: number, req: { email?: string; is_admin?: boolean }) =>
    call<unknown>("PATCH", `/api/users/${id}`, req),
  deleteUser: (id: number) => call<unknown>("DELETE", `/api/users/${id}`),

  // Categories --------------------------------------------------------
  listCategories: () => call<Category[]>("GET", "/api/categories"),
  createCategory: (req: { name: string; color?: string; position?: number }) =>
    call<Category>("POST", "/api/categories", req),
  updateCategory: (
    id: number,
    req: { name?: string; color?: string; position?: number },
  ) => call<unknown>("PATCH", `/api/categories/${id}`, req),
  deleteCategory: (id: number) => call<unknown>("DELETE", `/api/categories/${id}`),
  reorderCategories: (ids: number[]) =>
    call<unknown>("POST", "/api/categories/reorder", { ids }),

  // Feeds -------------------------------------------------------------
  listFeeds: () => call<FeedWithCounts[]>("GET", "/api/feeds"),
  addFeed: (url: string, category_id?: number) =>
    call<{ feed: FeedWithCounts; subscription: unknown }>("POST", "/api/feeds", {
      url,
      category_id,
    }),
  discoverFeeds: (url: string) =>
    call<{ feeds: DiscoveredFeed[] }>("POST", "/api/feeds/discover", { url }),
  updateFeed: (
    id: number,
    req: {
      title_override?: string;
      category_id?: number;
      clear_category?: boolean;
      muted?: boolean;
    },
  ) => call<unknown>("PATCH", `/api/feeds/${id}`, req),
  deleteFeed: (id: number) => call<unknown>("DELETE", `/api/feeds/${id}`),
  refreshFeed: (id: number) => call<unknown>("POST", `/api/feeds/${id}/refresh`),
  reorderFeeds: (ids: number[]) =>
    call<unknown>("POST", "/api/feeds/reorder", { ids }),
  resummarizeFeed: (id: number) =>
    call<{ reset: number; enqueued: number }>("POST", `/api/feeds/${id}/resummarize`),
  exportOPML: () => fetch("/api/feeds/export", { credentials: "include" }),
  importOPML: async (file: File): Promise<{ data: { imported: number } }> => {
    const form = new FormData();
    form.append("file", file);
    const headers: Record<string, string> = {};
    const tok = readCSRFCookie();
    if (tok) headers["X-Ember-CSRF"] = tok;
    const res = await fetch("/api/feeds/import", {
      method: "POST",
      credentials: "include",
      headers,
      body: form,
    });
    if (!res.ok) {
      let msg = res.statusText;
      try {
        const j = (await res.json()) as { error?: { message?: string } };
        if (j?.error?.message) msg = j.error.message;
      } catch {
        /* keep statusText */
      }
      throw new ApiError(res.status, "http_" + res.status, msg);
    }
    return res.json();
  },
  importTTRSS: async (
    file: File,
  ): Promise<{ data: { total: number; imported: number; skipped: number } }> => {
    const form = new FormData();
    form.append("file", file);
    const headers: Record<string, string> = {};
    const tok = readCSRFCookie();
    if (tok) headers["X-Ember-CSRF"] = tok;
    const res = await fetch("/api/feeds/import-ttrss", {
      method: "POST",
      credentials: "include",
      headers,
      body: form,
    });
    if (!res.ok) {
      let msg = res.statusText;
      try {
        const j = (await res.json()) as { error?: { message?: string } };
        if (j?.error?.message) msg = j.error.message;
      } catch {
        /* keep statusText */
      }
      throw new ApiError(res.status, "http_" + res.status, msg);
    }
    return res.json();
  },
  importTTRSSAPI: (body: {
    url: string;
    username: string;
    password: string;
    import_starred: boolean;
    import_archived: boolean;
  }) =>
    call<{ total: number; imported: number; skipped: number }>(
      "POST",
      "/api/feeds/import-ttrss-api",
      body,
    ),

  // Articles ----------------------------------------------------------
  listArticles: (q: ListArticlesQuery = {}) => {
    const sp = new URLSearchParams();
    Object.entries(q).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== "" && v !== false) {
        sp.set(k, String(v));
      }
    });
    const qs = sp.toString();
    return call<ArticleView[]>("GET", `/api/articles${qs ? "?" + qs : ""}`);
  },
  getArticle: (id: number) => call<ArticleView>("GET", `/api/articles/${id}`),
  getArticleCluster: (id: number) =>
    call<{ siblings: ClusterSibling[] }>("GET", `/api/articles/${id}/cluster`),
  setRead: (ids: number[], read: boolean) =>
    call<{ count: number }>("POST", "/api/articles/read", { ids, read }),
  setStar: (id: number, value: boolean) =>
    call<unknown>("POST", "/api/articles/star", { id, value }),
  setLater: (id: number, value: boolean) =>
    call<unknown>("POST", "/api/articles/later", { id, value }),
  markAllRead: (req: { feed_id?: number; category_id?: number; view?: string }) =>
    call<{ count: number }>("POST", "/api/articles/mark-all-read", req),

  // Filters -----------------------------------------------------------
  listFilters: () => call<Filter[]>("GET", "/api/filters"),
  createFilter: (req: {
    name: string;
    match_json: string;
    action: "mark_read" | "star" | "hide";
    enabled?: boolean;
  }) => call<Filter>("POST", "/api/filters", req),
  updateFilter: (
    id: number,
    req: { name?: string; match_json?: string; action?: string; enabled?: boolean },
  ) => call<unknown>("PATCH", `/api/filters/${id}`, req),
  deleteFilter: (id: number) => call<unknown>("DELETE", `/api/filters/${id}`),

  // Boards ------------------------------------------------------------
  listBoards: () => call<Board[]>("GET", "/api/boards"),
  createBoard: (name: string) => call<Board>("POST", "/api/boards", { name }),
  deleteBoard: (id: number) => call<unknown>("DELETE", `/api/boards/${id}`),
  addToBoard: (boardId: number, articleId: number) =>
    call<unknown>("POST", `/api/boards/${boardId}/articles`, { article_id: articleId }),
  removeFromBoard: (boardId: number, articleId: number) =>
    call<unknown>("DELETE", `/api/boards/${boardId}/articles/${articleId}`),

  // Shares ------------------------------------------------------------
  createShare: (article_id: number, to_user: number, note?: string) =>
    call<Share>("POST", "/api/shares", { article_id, to_user, note }),
  inbox: (unseenOnly = false) =>
    call<Share[]>("GET", `/api/shares/inbox${unseenOnly ? "?unseen=1" : ""}`),
  markShareSeen: (id: number) =>
    call<unknown>("POST", `/api/shares/${id}/seen`),

  // Search ------------------------------------------------------------
  search: (q: string, limit = 30) =>
    call<SearchResult[]>("GET", `/api/search?q=${encodeURIComponent(q)}&limit=${limit}`),
  listSavedSearches: () => call<SavedSearch[]>("GET", "/api/saved-searches"),
  createSavedSearch: (name: string, query: string) =>
    call<SavedSearch>("POST", "/api/saved-searches", { name, query }),
  deleteSavedSearch: (id: number) =>
    call<unknown>("DELETE", `/api/saved-searches/${id}`),

  // Per-article tags ------------------------------------------------
  listArticleTags: (articleID: number) =>
    call<string[]>("GET", `/api/articles/${articleID}/tags`),
  addArticleTag: (articleID: number, tag: string) =>
    call<string[]>("POST", `/api/articles/${articleID}/tags`, { tag }),
  removeArticleTag: (articleID: number, tag: string) =>
    call<string[]>("DELETE", `/api/articles/${articleID}/tags?tag=${encodeURIComponent(tag)}`),
  listUserTags: () => call<{ tag: string; count: number }[]>("GET", "/api/tags"),

  // Reading stats ---------------------------------------------------
  getStats: () => call<UserStats>("GET", "/api/me/stats"),

  // Sidebar smart-view badge counts (Fresh / Starred / Later / Shared).
  getSmartCounts: () =>
    call<SmartCounts>("GET", "/api/me/smart-counts"),

  // Daily digest -----------------------------------------------------
  getDigest: () => call<UserDigest>("GET", "/api/me/digest"),
  setDigest: (d: Partial<UserDigest>) =>
    call<UserDigest>("POST", "/api/me/digest", d),

  // Starter packs ----------------------------------------------------
  listStarterPacks: () =>
    call<StarterPack[]>("GET", "/api/starter-packs"),
  importStarterPack: (slug: string) =>
    call<StarterImportResult>("POST", `/api/starter-packs/${slug}`),
  removeStarterPack: (slug: string) =>
    call<StarterRemoveResult>("DELETE", `/api/starter-packs/${slug}`),

  // LLM admin --------------------------------------------------------
  getLLMStatus: () => call<LLMStatus>("GET", "/api/admin/llm"),
  setLLMModel: (model: string) =>
    call<{ model: string }>("POST", "/api/admin/llm/model", { model }),
  pullLLMModel: (model: string) =>
    call<{ model: string }>("POST", "/api/admin/llm/pull", { model }),
  deleteLLMModel: (model: string) =>
    call<{ model: string }>("POST", "/api/admin/llm/delete", { model }),
  setLLMOptions: (opts: LLMOptions) =>
    call<LLMOptions>("POST", "/api/admin/llm/options", opts),

  // Branding ---------------------------------------------------------
  getBranding: () => call<BrandingDTO>("GET", "/api/branding"),
  setBranding: (b: Partial<BrandingDTO>) =>
    call<BrandingDTO>("POST", "/api/admin/branding", b),

  // DB admin --------------------------------------------------------
  getDBStatus: () => call<DBStatus>("GET", "/api/admin/db"),
  dbBackup: () => call<DBBackup>("POST", "/api/admin/db/backup"),
  dbCleanup: (older_days: number) =>
    call<DBCleanupStats>("POST", "/api/admin/db/cleanup", { older_days }),
  dbSchedule: (s: DBSchedule) =>
    call<{ ok: string }>("POST", "/api/admin/db/schedule", s),

  // Admin: session TTL ----------------------------------------------
  getSessionTTL: () => call<SessionTTL>("GET", "/api/admin/session"),
  setSessionTTL: (ttl_seconds: number) =>
    call<SessionTTL>("POST", "/api/admin/session/ttl", { ttl_seconds }),

  // Admin: settings (SMTP + initial backlog window) ----------------
  getAdminSettings: () => call<AdminSettings>("GET", "/api/admin/settings"),
  setAdminSettings: (patch: AdminSettingsPatch) =>
    call<AdminSettings>("PATCH", "/api/admin/settings", patch),
  testEmail: (to?: string) =>
    call<{ sent_to: string }>("POST", "/api/admin/settings/email-test", to ? { to } : {}),

  // Article: on-demand readability re-extract -----------------------
  // Returns either the updated article (status=ok) OR a no-change marker when
  // readability ran but produced nothing better than the stored body. Both
  // come back as 200; the caller checks meta.status to decide UX.
  reExtractArticle: (id: number) =>
    call<import("./types").ArticleView | { status: "no_change" }>(
      "POST",
      `/api/articles/${id}/extract`,
    ),

  // Web Push (VAPID) ------------------------------------------------
  pushVapidKey: () =>
    call<{ public_key: string }>("GET", "/api/me/push-vapid-public-key"),
  pushSubscriptions: () =>
    call<PushSubscriptionSummary[]>("GET", "/api/me/push-subscriptions"),
  pushSubscribe: (req: {
    endpoint: string;
    p256dh: string;
    auth: string;
    user_agent: string;
  }) => call<{ id: number }>("POST", "/api/me/push-subscriptions", req),
  pushUnsubscribe: (id: number) =>
    call<{ ok: boolean }>("DELETE", `/api/me/push-subscriptions/${id}`),
  pushTest: () =>
    call<{ sent: number; removed: number }>("POST", "/api/me/push-subscriptions/test"),

  // Passkeys --------------------------------------------------------
  listPasskeys: () => call<PasskeySummary[]>("GET", "/api/me/passkeys"),
  passkeyRegisterBegin: () =>
    call<PasskeyChallenge>("POST", "/api/me/passkeys/register/begin"),
  passkeyRegisterFinish: (session_id: string, name: string, response: unknown) =>
    call<PasskeySummary>("POST", "/api/me/passkeys/register/finish", {
      session_id,
      name,
      response,
    }),
  renamePasskey: (id: number, name: string) =>
    call<unknown>("PATCH", `/api/me/passkeys/${id}`, { name }),
  deletePasskey: (id: number) =>
    call<unknown>("DELETE", `/api/me/passkeys/${id}`),
  passkeyLoginBegin: (username: string) =>
    call<PasskeyChallenge>("POST", "/api/auth/passkey/begin", { username }),
  passkeyLoginFinish: (session_id: string, response: unknown) =>
    call<User>("POST", "/api/auth/passkey/finish", { session_id, response }),
  // System-wide "any passkey on this server?" — used by Login.svelte to hide
  // the passkey button when no user has registered one yet. Returns
  // {any_registered: false} when WebAuthn isn't configured either, so the
  // UI can treat both cases the same way.
  passkeyAnyRegistered: () =>
    call<{ any_registered: boolean }>("GET", "/api/auth/passkey/exists"),
};

export interface PasskeySummary {
  id: number;
  name: string;
  created_at: number;
  last_used_at: number;
}
export interface PasskeyChallenge {
  session_id: string;
  // PublicKeyCredentialCreationOptions / RequestOptions JSON. Has a top-level
  // `publicKey` field per the WebAuthn API.
  options: { publicKey: PublicKeyCredentialCreationOptionsJSON | PublicKeyCredentialRequestOptionsJSON };
}
// Minimal local types covering the fields we need to convert. The library
// returns spec JSON with base64url-encoded ArrayBuffers; we round-trip them
// in passkey.ts.
export interface PublicKeyCredentialCreationOptionsJSON {
  challenge: string;
  user: { id: string; name: string; displayName: string };
  excludeCredentials?: { id: string; type: string; transports?: string[] }[];
  [k: string]: unknown;
}
export interface PublicKeyCredentialRequestOptionsJSON {
  challenge: string;
  allowCredentials?: { id: string; type: string; transports?: string[] }[];
  [k: string]: unknown;
}

export interface DBBackup {
  path: string;
  size_bytes: number;
  created_at: number;
}
export interface DBCleanupStats {
  articles_deleted: number;
  bytes_reclaimed: number;
}
export interface DBSchedule {
  backup_schedule: "off" | "daily" | "weekly";
  backup_keep_count: number;
  cleanup_schedule: "off" | "weekly" | "monthly";
  cleanup_older_days: number;
  opml_schedule?: "off" | "weekly" | "monthly";
}
export interface DBStatus extends DBSchedule {
  size_bytes: number;
  page_count: number;
  backup_dir: string;
  backups: DBBackup[];
}

// Admin server-wide session TTL. Source = 'admin' means an admin saved
// a value via Settings; 'default' means we're using auth.DefaultSessionTTL
// or whatever EMBER_SESSION_TTL bootstrapped.
export interface SessionTTL {
  ttl_seconds: number;
  source: "admin" | "default";
}

// AdminSettings is the read-back shape from GET /api/admin/settings. The SMTP
// password is never echoed; password_set is a boolean so the UI can show
// "stored ✓" and offer a Clear control.
export interface AdminSettings {
  smtp: {
    host: string;
    port: number;
    username: string;
    password_set: boolean;
    from: string;
    starttls: boolean;
  };
  initial_backlog_hours: number;
}

// AdminSettingsPatch mirrors the backend's pointer-bag: only fields included
// are updated. To clear the SMTP password, send `clear_password: true`.
export interface AdminSettingsPatch {
  smtp?: {
    host?: string;
    port?: number;
    username?: string;
    password?: string;
    clear_password?: boolean;
    from?: string;
    starttls?: boolean;
  };
  initial_backlog_hours?: number;
}

export interface TopFeed {
  feed_id: number;
  title: string;
  read_count: number;
}
export interface UserStats {
  articles_read_today: number;
  articles_read_week: number;
  articles_read_month: number;
  starred_total: number;
  later_total: number;
  subscriptions: number;
  top_feeds: TopFeed[] | null;
}

// Mirrors store.SmartViewCounts — populated by GET /api/me/smart-counts and
// stored alongside the sidebar feeds list.
export interface SmartCounts {
  fresh: number;
  starred: number;
  later: number;
  shared: number;
  // pending_summary: articles awaiting LLM summarization. Drives the
  // "Summarizing N articles" indicator at the bottom of the sidebar.
  // 0 → indicator hidden.
  pending_summary: number;
}

export interface UserDigest {
  user_id: number;
  enabled: boolean;
  view_kind: "smart" | "feed" | "category" | "board";
  view_value: string;
  hour_utc: number;
  minute_utc: number;
  last_sent_at: number;
  email_override: string;
}

export interface BrandingDTO {
  name: string;
  page_title: string;
  favicon_url: string;
}

export interface LLMSystemInfo {
  ram_bytes: number;
  cpus: number;
  gpu: string;
  os: string;
}
export interface LLMRecommendation {
  model: string;
  reason: string;
  disable_llm: boolean;
}
export interface LLMInstalledModel {
  name: string;
  size_bytes: number;
  modified_at: string;
}
export interface LLMOptions {
  temperature: number;
  top_p: number;
  num_ctx: number;
}
export interface LLMStatus {
  current_model: string;
  enabled: boolean;
  system: LLMSystemInfo;
  recommended: LLMRecommendation;
  installed?: LLMInstalledModel[];
  installed_err?: string;
  options: LLMOptions;
}

export interface StarterPack {
  slug: string;
  name: string;
  color: string;
  feed_urls: string[];
  // Count of pack feeds the requesting user is already subscribed to. The UI
  // flips the Add/Remove button on `subscribed === feed_urls.length`.
  subscribed: number;
}
export interface StarterImportResult {
  pack: string;
  category_id: number;
  feeds_added: number;
  already_had: number;
  failed_urls?: string[];
}
export interface StarterRemoveResult {
  pack: string;
  feeds_removed: number;
  not_subscribed: number;
  // True when the empty pack-category was deleted after the last feed was
  // unsubscribed. False if the user had added their own feeds to it (kept).
  category_removed: boolean;
}
