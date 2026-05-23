// Typed fetch client for the ember API. Throws ApiError on non-2xx.

import type {
  ArticleView,
  Board,
  Category,
  FeedWithCounts,
  ListArticlesQuery,
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
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
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
  me: () => call<User>("GET", "/api/me"),
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

  // Feeds -------------------------------------------------------------
  listFeeds: () => call<FeedWithCounts[]>("GET", "/api/feeds"),
  addFeed: (url: string, category_id?: number) =>
    call<{ feed: FeedWithCounts; subscription: unknown }>("POST", "/api/feeds", {
      url,
      category_id,
    }),
  updateFeed: (
    id: number,
    req: { title_override?: string; category_id?: number; clear_category?: boolean },
  ) => call<unknown>("PATCH", `/api/feeds/${id}`, req),
  deleteFeed: (id: number) => call<unknown>("DELETE", `/api/feeds/${id}`),
  refreshFeed: (id: number) => call<unknown>("POST", `/api/feeds/${id}/refresh`),
  exportOPML: () => fetch("/api/feeds/export", { credentials: "include" }),

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
};
