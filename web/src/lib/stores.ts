import { writable, derived, get } from "svelte/store";
import type {
  ArticleView,
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

export async function refreshMe(): Promise<User | null> {
  try {
    const res = await api.me();
    user.set(res.data.user);
    feverAPIKey.set(res.data.fever_api_key);
    appVersion.set(res.data.version);
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

// Feeds / categories ---------------------------------------------------------
export const feeds = writable<FeedWithCounts[]>([]);
export const categories = writable<Category[]>([]);

export async function refreshSidebar(): Promise<void> {
  const [f, c] = await Promise.all([api.listFeeds(), api.listCategories()]);
  feeds.set(f.data ?? []);
  categories.set(c.data ?? []);
}

export const totalUnread = derived(feeds, ($feeds) =>
  $feeds.reduce((n, f) => n + (f.unread || 0), 0),
);

// View / UI state ------------------------------------------------------------
export type ActiveView =
  | { kind: "smart"; view: "fresh" | "today" | "unread" | "starred" | "later" | "shared" }
  | { kind: "feed"; id: number }
  | { kind: "category"; id: number };

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
export const theme = writable<"light" | "dark">(loadPref("ember:theme", "light"));
export const density = writable<"card" | "compact">(loadPref("ember:density", "card"));

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
  }
}

export async function loadArticles(view: ActiveView, append = false): Promise<void> {
  articles.update((s) => ({ ...s, loading: true, err: undefined }));
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
  } catch (err) {
    articles.update((s) => ({ ...s, loading: false, err: String(err) }));
  }
}

// Read/star toggles update the local list optimistically so the UI feels snappy.
export async function setRead(ids: number[], read: boolean): Promise<void> {
  articles.update((s) => ({
    ...s,
    items: s.items.map((a) => (ids.includes(a.id) ? { ...a, is_read: read } : a)),
  }));
  await api.setRead(ids, read);
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

export async function toggleStar(id: number, value: boolean): Promise<void> {
  articles.update((s) => ({
    ...s,
    items: s.items.map((a) => (a.id === id ? { ...a, is_starred: value } : a)),
  }));
  await api.setStar(id, value);
}

export async function toggleLater(id: number, value: boolean): Promise<void> {
  articles.update((s) => ({
    ...s,
    items: s.items.map((a) => (a.id === id ? { ...a, is_later: value } : a)),
  }));
  await api.setLater(id, value);
}
