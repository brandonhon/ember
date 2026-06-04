// Wire types matching internal/models. Keep in sync.

export interface User {
  id: number;
  username: string;
  email?: string;
  is_admin: boolean;
  settings_json: string;
  created_at: number;
}

// MeResponse is the envelope /api/me returns: the user plus derived fields
// the SPA needs (Fever api_key for mobile clients, app version).
export interface MeResponse {
  user: User;
  fever_api_key: string;
  version: string;
  // Server-configured Fresh-view cutoff in seconds (EMBER_FRESH_WINDOW).
  // ArticleList.svelte's isFresh() uses this so the badge + filter stay
  // consistent with the server's CountSmartViews query. Defaults to 6h
  // on the server if unset, but the client also defaults to 6h on
  // missing/zero to be defensive.
  fresh_window_seconds: number;
}

export interface Category {
  id: number;
  user_id: number;
  name: string;
  color?: string;
  position: number;
  created_at: number;
}

export interface Feed {
  id: number;
  url: string;
  site_url?: string;
  title: string;
  favicon_url?: string;
  last_fetched?: number;
  next_fetch?: number;
  fetch_interval: number;
  error_count: number;
  last_error?: string;
  created_at: number;
}

export interface FeedWithCounts extends Feed {
  subscription_id: number;
  category_id?: number;
  title_override?: string;
  muted: boolean;
  position: number;
  unread: number;
}

/** A feed surfaced by POST /api/feeds/discover; not yet subscribed. */
export interface DiscoveredFeed {
  url: string;
  title: string;
  type: string; // "rss", "atom", or "" when unknown
}

export interface Article {
  id: number;
  feed_id: number;
  guid: string;
  url?: string;
  title: string;
  author?: string;
  content_html?: string;
  cleaned_html?: string;
  content_text?: string;
  summary?: string;
  summary_model?: string;
  image_url?: string;
  published_at?: number;
  fetched_at: number;
  content_hash: string;
  tags?: string;
}

export interface ArticleView extends Article {
  is_read: boolean;
  is_starred: boolean;
  is_later: boolean;
  // Count of other articles with the same URL the user is subscribed to via
  // different feeds (cross-feed dedup kept the lowest-id row). 0 = unique.
  dup_count: number;
}

export interface SavedSearch {
  id: number;
  user_id: number;
  name: string;
  query: string;
  created_at: number;
}

export interface Filter {
  id: number;
  user_id: number;
  name: string;
  match_json: string;
  action: "mark_read" | "star" | "hide";
  enabled: boolean;
  created_at: number;
}

export interface FilterMatch {
  field: "title" | "content" | "author" | "url";
  op: "contains" | "equals" | "starts_with" | "matches";
  value: string;
  case_sensitive?: boolean;
}

export interface Board {
  id: number;
  user_id: number;
  name: string;
  created_at: number;
}

export interface Share {
  id: number;
  article_id: number;
  from_user: number;
  to_user: number;
  note?: string;
  created_at: number;
  seen: boolean;
}

export interface SearchResult extends ArticleView {
  rank: number;
}

export type ArticleView_View =
  | "today"
  | "fresh"
  | "unread"
  | "starred"
  | "later"
  | "shared"
  | "";

export interface ListArticlesQuery {
  view?: ArticleView_View;
  feed_id?: number;
  category_id?: number;
  board_id?: number;
  unread?: boolean;
  starred?: boolean;
  later?: boolean;
  fresh_after?: number;
  limit?: number;
  cursor_pub?: number;
  cursor_id?: number;
}
