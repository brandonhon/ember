// Wire types matching internal/models. Keep in sync.

export interface User {
  id: number;
  username: string;
  email?: string;
  is_admin: boolean;
  settings_json: string;
  created_at: number;
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
  unread: number;
}

export interface Article {
  id: number;
  feed_id: number;
  guid: string;
  url?: string;
  title: string;
  author?: string;
  content_html?: string;
  content_text?: string;
  summary?: string;
  summary_model?: string;
  image_url?: string;
  published_at?: number;
  fetched_at: number;
  content_hash: string;
}

export interface ArticleView extends Article {
  is_read: boolean;
  is_starred: boolean;
  is_later: boolean;
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
