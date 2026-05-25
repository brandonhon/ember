// Typed fetch client for the ember API. Throws ApiError on non-2xx.

import type {
  ArticleView,
  Board,
  Category,
  FeedWithCounts,
  Filter,
  ListArticlesQuery,
  MeResponse,
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

  // Starter packs ----------------------------------------------------
  listStarterPacks: () =>
    call<StarterPack[]>("GET", "/api/starter-packs"),
  importStarterPack: (slug: string) =>
    call<StarterImportResult>("POST", `/api/starter-packs/${slug}`),

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
};

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
  base_url: string;
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
}
export interface StarterImportResult {
  pack: string;
  category_id: number;
  feeds_added: number;
  already_had: number;
  failed_urls?: string[];
}
