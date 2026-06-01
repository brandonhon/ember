// Package models defines the data types shared across the store, API, and
// poller. Timestamps are Unix seconds.
package models

// User represents an ember account.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email,omitempty"`
	PasswordHash string `json:"-"`
	IsAdmin      bool   `json:"is_admin"`
	SettingsJSON string `json:"settings_json"`
	// FeverToken is the random API key clients use for the Fever shim. Not
	// exposed in admin user listings; only the owning user sees it via
	// /api/me. Empty until backfilled on first /api/me hit.
	FeverToken string `json:"-"`
	CreatedAt  int64  `json:"created_at"`
}

// Session is a server-side row backing a session cookie.
type Session struct {
	ID        string `json:"id"`
	UserID    int64  `json:"user_id"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
	UserAgent string `json:"user_agent,omitempty"`
}

// Feed is a syndication source. Shared across all users who subscribe.
type Feed struct {
	ID            int64  `json:"id"`
	URL           string `json:"url"`
	SiteURL       string `json:"site_url,omitempty"`
	Title         string `json:"title"`
	FaviconURL    string `json:"favicon_url,omitempty"`
	ETag          string `json:"etag,omitempty"`
	LastModified  string `json:"last_modified,omitempty"`
	LastFetched   int64  `json:"last_fetched,omitempty"`
	NextFetch     int64  `json:"next_fetch,omitempty"`
	FetchInterval int64  `json:"fetch_interval"`
	ErrorCount    int    `json:"error_count"`
	LastError     string `json:"last_error,omitempty"`
	CreatedAt     int64  `json:"created_at"`
}

// Category is a user-scoped folder.
type Category struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Color     string `json:"color,omitempty"`
	Position  int    `json:"position"`
	CreatedAt int64  `json:"created_at"`
}

// Subscription links a user to a feed, optionally filed under a category.
type Subscription struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"user_id"`
	FeedID        int64  `json:"feed_id"`
	CategoryID    *int64 `json:"category_id,omitempty"`
	TitleOverride string `json:"title_override,omitempty"`
	Muted         bool   `json:"muted"`
	Position      int    `json:"position"`
	CreatedAt     int64  `json:"created_at"`
}

// Article is a single item ingested from a feed. Shared storage across users.
type Article struct {
	ID          int64  `json:"id"`
	FeedID      int64  `json:"feed_id"`
	GUID        string `json:"guid"`
	URL         string `json:"url,omitempty"`
	Title       string `json:"title"`
	Author      string `json:"author,omitempty"`
	ContentHTML string `json:"content_html,omitempty"`
	ContentText string `json:"content_text,omitempty"`
	// CleanedHTML is the LLM-produced version of ContentHTML with promo
	// content (newsletter signups, podcast/app promos) stripped. Empty when
	// summaries are disabled or the model didn't return a CLEANED section.
	// Reader prefers this over ContentHTML when present.
	CleanedHTML  string `json:"cleaned_html,omitempty"`
	Summary      string `json:"summary,omitempty"`
	SummaryModel string `json:"summary_model,omitempty"`
	ImageURL     string `json:"image_url,omitempty"`
	PublishedAt  int64  `json:"published_at,omitempty"`
	FetchedAt    int64  `json:"fetched_at"`
	ContentHash  string `json:"content_hash"`
	Tags         string `json:"tags,omitempty"` // comma-joined gofeed categories; first one used as a badge.
	// CanonicalURL is the tracking-param-stripped, case-normalized form of
	// URL used for cross-feed dedup. Populated at ingest by feed.CanonicalURL.
	// Internal — never exposed to clients (the human-readable URL stays in URL).
	CanonicalURL string `json:"-"`
	// ClusterID is a stable short hash of CanonicalURL. Empty when URL is
	// empty. Drives the cross-feed dedup join and identifies the "this
	// article also appears in N other feeds" sibling set.
	ClusterID string `json:"-"`
	// TitleFingerprint is a normalized form of Title used for soft
	// clustering when canonical URLs differ but headlines match (typical
	// wire-story syndication). Empty for titles that are too short /
	// generic to be reliable cluster keys (e.g. "Re:", "News update").
	// Internal only; never exposed via the API.
	TitleFingerprint string `json:"-"`
}

// ArticleState is per-user read/star/later state for an article.
type ArticleState struct {
	UserID    int64 `json:"user_id"`
	ArticleID int64 `json:"article_id"`
	IsRead    bool  `json:"is_read"`
	IsStarred bool  `json:"is_starred"`
	IsLater   bool  `json:"is_later"`
	ReadAt    int64 `json:"read_at,omitempty"`
	StarredAt int64 `json:"starred_at,omitempty"`
}

// Board is a user-scoped curated collection.
type Board struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

// SavedSearch is a persisted FTS query that the user can re-run from the
// sidebar. Acts like a smart view backed by /api/search.
type SavedSearch struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Query     string `json:"query"`
	CreatedAt int64  `json:"created_at"`
}

// UserDigest is the per-user daily-digest configuration. ViewKind is one of
// smart|feed|category|board and ViewValue is the smart-view name or numeric
// id, matching the ActiveView union the SPA uses.
type UserDigest struct {
	UserID        int64  `json:"user_id"`
	Enabled       bool   `json:"enabled"`
	ViewKind      string `json:"view_kind"`
	ViewValue     string `json:"view_value"`
	HourUTC       int    `json:"hour_utc"`
	MinuteUTC     int    `json:"minute_utc"`
	LastSentAt    int64  `json:"last_sent_at"`
	EmailOverride string `json:"email_override"`
}

// Filter is a user rule applied to incoming articles.
type Filter struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	MatchJSON string `json:"match_json"`
	Action    string `json:"action"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
	// Priority orders rule evaluation — lower numbers win when two rules
	// would set conflicting state on the same article. Default 100;
	// arbitrary positive integers allowed.
	Priority int `json:"priority"`
	// ActionValue carries the payload for actions that need one. For
	// ActionTag it's the tag name to attach. For ActionAddToBoard it's
	// the board id as a decimal string (stringly typed to keep the
	// schema simple and forward-compatible). Empty for actions that
	// don't need a payload (mark_read, star, hide).
	ActionValue string `json:"action_value"`
}

// Share is one article shared from one user to another.
type Share struct {
	ID        int64  `json:"id"`
	ArticleID int64  `json:"article_id"`
	FromUser  int64  `json:"from_user"`
	ToUser    int64  `json:"to_user"`
	Note      string `json:"note,omitempty"`
	CreatedAt int64  `json:"created_at"`
	Seen      bool   `json:"seen"`
}

// ArticleView is what list/feed handlers return: the article joined with this
// user's read/star/later state.
type ArticleView struct {
	Article
	IsRead    bool `json:"is_read"`
	IsStarred bool `json:"is_starred"`
	IsLater   bool `json:"is_later"`
	// DupCount counts other articles with the same URL that the user is
	// subscribed to via a different feed. 0 means no duplicates. The UI shows
	// a pill ("Also in 2 feeds") when this is > 0.
	DupCount int `json:"dup_count"`
}

// Passkey is one WebAuthn credential bound to a user.
type Passkey struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"user_id"`
	CredentialID   []byte `json:"-"`
	PublicKey      []byte `json:"-"`
	AttestationTyp string `json:"-"`
	AAGUID         []byte `json:"-"`
	SignCount      uint32 `json:"-"`
	Transports     string `json:"transports,omitempty"`
	BackupEligible bool   `json:"backup_eligible"`
	BackupState    bool   `json:"backup_state"`
	Name           string `json:"name"`
	CreatedAt      int64  `json:"created_at"`
	LastUsedAt     int64  `json:"last_used_at"`
}

// FeedWithCounts is a feed joined with the requesting user's subscription
// metadata and unread count.
type FeedWithCounts struct {
	Feed
	SubscriptionID int64  `json:"subscription_id"`
	CategoryID     *int64 `json:"category_id,omitempty"`
	TitleOverride  string `json:"title_override,omitempty"`
	Muted          bool   `json:"muted"`
	Position       int    `json:"position"`
	Unread         int    `json:"unread"`
}
