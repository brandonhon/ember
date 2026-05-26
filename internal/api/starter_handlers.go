package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
	"github.com/go-chi/chi/v5"
)

// StarterPack is a curated set of feeds an admin or new user can import in a
// single click. Slugs are stable; categories are created if missing.
type StarterPack struct {
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
	Color    string   `json:"color"`
	FeedURLs []string `json:"feed_urls"`
}

// starterPacks is the curated list. Five categories, 3-5 feeds each. URLs are
// kept as the canonical feed endpoints; the poller resolves titles + favicons
// on the first successful fetch.
var starterPacks = []StarterPack{
	{
		Slug:  "technology",
		Name:  "Technology",
		Color: "#a93b16",
		FeedURLs: []string{
			"https://hnrss.org/frontpage",
			"https://feeds.arstechnica.com/arstechnica/index",
			"https://www.theverge.com/rss/index.xml",
			"https://lwn.net/headlines/rss",
		},
	},
	{
		Slug:  "programming",
		Name:  "Programming",
		Color: "#1d4ed8",
		FeedURLs: []string{
			"https://go.dev/blog/feed.atom",
			"https://lobste.rs/rss",
			"https://engineering.fb.com/feed/",
			"https://stackoverflow.blog/feed/",
		},
	},
	{
		Slug:  "security",
		Name:  "Security",
		Color: "#991b1b",
		FeedURLs: []string{
			"https://krebsonsecurity.com/feed/",
			"https://www.schneier.com/feed/atom/",
			"https://www.cisa.gov/cybersecurity-advisories/all.xml",
			"https://feeds.feedburner.com/TheHackersNews",
		},
	},
	{
		Slug:  "devops",
		Name:  "DevOps & Infra",
		Color: "#0a7b3a",
		FeedURLs: []string{
			"https://www.hashicorp.com/blog/feed.xml",
			"https://kubernetes.io/feed.xml",
			"https://aws.amazon.com/about-aws/whats-new/recent/feed/",
			"https://www.docker.com/blog/feed/",
		},
	},
	{
		Slug:  "world",
		Name:  "World News",
		Color: "#623ce6",
		FeedURLs: []string{
			"http://feeds.bbci.co.uk/news/world/rss.xml",
			"https://feeds.npr.org/1004/rss.xml",
			"https://www.aljazeera.com/xml/rss/all.xml",
			"https://www.theguardian.com/world/rss",
		},
	},
}

// starterPackView is the per-user list response: the pack metadata plus how
// many of its feeds the caller is currently subscribed to. The UI uses
// `subscribed == len(feed_urls)` to flip the button between Add and Remove.
type starterPackView struct {
	StarterPack
	Subscribed int `json:"subscribed"`
}

func (d *Dependencies) handleListStarterPacks(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	subs, err := d.Store.ListFeedsForUser(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	have := make(map[string]bool, len(subs))
	for _, f := range subs {
		have[f.URL] = true
	}
	views := make([]starterPackView, len(starterPacks))
	for i, p := range starterPacks {
		v := starterPackView{StarterPack: p}
		for _, url := range p.FeedURLs {
			if have[url] {
				v.Subscribed++
			}
		}
		views[i] = v
	}
	writeData(w, http.StatusOK, views, nil)
}

type starterImportResult struct {
	Pack       string   `json:"pack"`
	CategoryID int64    `json:"category_id"`
	FeedsAdded int      `json:"feeds_added"`
	AlreadyHad int      `json:"already_had"`
	FailedURLs []string `json:"failed_urls,omitempty"`
}

type starterRemoveResult struct {
	Pack            string `json:"pack"`
	FeedsRemoved    int    `json:"feeds_removed"`
	NotSubscribed   int    `json:"not_subscribed"`
	CategoryRemoved bool   `json:"category_removed"`
}

// handleImportStarterPack creates the named category (or reuses an existing
// one with the same name) and subscribes the user to every feed in the pack.
// Existing subscriptions are skipped — the operation is idempotent.
func (d *Dependencies) handleImportStarterPack(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	slug := chi.URLParam(r, "slug")
	var pack *StarterPack
	for i := range starterPacks {
		if starterPacks[i].Slug == slug {
			pack = &starterPacks[i]
			break
		}
	}
	if pack == nil {
		writeError(w, http.StatusNotFound, "not_found", "starter pack not found")
		return
	}

	ctx := r.Context()

	// Find or create the category.
	cats, err := d.Store.ListCategories(ctx, u.ID)
	if mapStoreError(w, err) {
		return
	}
	var categoryID int64
	for _, c := range cats {
		if strings.EqualFold(c.Name, pack.Name) {
			categoryID = c.ID
			break
		}
	}
	if categoryID == 0 {
		c, err := d.Store.CreateCategory(ctx, models.Category{
			UserID: u.ID, Name: pack.Name, Color: pack.Color,
		})
		if mapStoreError(w, err) {
			return
		}
		categoryID = c.ID
	}

	// Subscribe to each feed; track already-had vs newly-added.
	existing, err := d.Store.ListFeedsForUser(ctx, u.ID)
	if mapStoreError(w, err) {
		return
	}
	have := make(map[string]bool, len(existing))
	for _, f := range existing {
		have[f.URL] = true
	}

	result := starterImportResult{Pack: pack.Slug, CategoryID: categoryID}
	for _, url := range pack.FeedURLs {
		if have[url] {
			result.AlreadyHad++
			continue
		}
		f, err := d.Store.UpsertFeed(ctx, models.Feed{URL: url, Title: url})
		if err != nil {
			result.FailedURLs = append(result.FailedURLs, url)
			continue
		}
		cid := categoryID
		if _, err := d.Store.Subscribe(ctx, models.Subscription{
			UserID: u.ID, FeedID: f.ID, CategoryID: &cid,
		}); err != nil {
			result.FailedURLs = append(result.FailedURLs, url)
			continue
		}
		result.FeedsAdded++
		// Best-effort initial refresh — don't block the response on the
		// network. Detaches from the request context (so the fetch survives
		// the handler return) but uses a bounded timeout. Derives from
		// d.backgroundCtx() (the server-level shutdown context) so we never
		// keep hitting the DB after dbh.Close().
		if d.Poller != nil {
			feedID := f.ID
			go func() {
				rctx, cancel := context.WithTimeout(d.backgroundCtx(), 60*time.Second)
				defer cancel()
				_ = d.Poller.RefreshFeed(rctx, feedID)
			}()
		}
	}

	writeData(w, http.StatusOK, result, nil)
}

// handleRemoveStarterPack unsubscribes the user from every feed in the pack
// that they currently have a subscription to. Idempotent — calling it twice
// (or on a pack they never installed) is harmless. Orphan feed rows are
// cleaned up automatically by Store.Unsubscribe when the user was the sole
// subscriber. The category created by import is deleted only if zero
// subscriptions remain under it — the user may have added their own feeds.
func (d *Dependencies) handleRemoveStarterPack(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	slug := chi.URLParam(r, "slug")
	var pack *StarterPack
	for i := range starterPacks {
		if starterPacks[i].Slug == slug {
			pack = &starterPacks[i]
			break
		}
	}
	if pack == nil {
		writeError(w, http.StatusNotFound, "not_found", "starter pack not found")
		return
	}

	ctx := r.Context()

	// Capture the pack's category ID before unsubscribing so we can check
	// whether it should be removed afterward. Matched by name (same lookup
	// import uses); may not exist if the user renamed/deleted it.
	cats, err := d.Store.ListCategories(ctx, u.ID)
	if mapStoreError(w, err) {
		return
	}
	var packCategoryID int64
	for _, c := range cats {
		if strings.EqualFold(c.Name, pack.Name) {
			packCategoryID = c.ID
			break
		}
	}

	subs, err := d.Store.ListFeedsForUser(ctx, u.ID)
	if mapStoreError(w, err) {
		return
	}
	subByURL := make(map[string]int64, len(subs))
	for _, f := range subs {
		subByURL[f.URL] = f.SubscriptionID
	}

	result := starterRemoveResult{Pack: pack.Slug}
	for _, url := range pack.FeedURLs {
		subID, ok := subByURL[url]
		if !ok {
			result.NotSubscribed++
			continue
		}
		if err := d.Store.Unsubscribe(ctx, u.ID, subID); err != nil {
			// Surface DB-level failures; partial removals on a malformed
			// pack are user-visible and worth knowing about.
			internalError(w, "starter/remove", err)
			return
		}
		result.FeedsRemoved++
	}

	// If the pack's category is now empty (no user-added feeds remain),
	// delete it so the sidebar doesn't keep a vestigial folder. Skip when
	// no category was resolved (already removed manually, etc).
	if packCategoryID != 0 {
		remaining, err := d.Store.ListFeedsForUser(ctx, u.ID)
		if mapStoreError(w, err) {
			return
		}
		empty := true
		for _, f := range remaining {
			if f.CategoryID != nil && *f.CategoryID == packCategoryID {
				empty = false
				break
			}
		}
		if empty {
			if err := d.Store.DeleteCategory(ctx, u.ID, packCategoryID); err == nil {
				result.CategoryRemoved = true
			}
			// Soft-fail: a category-delete error doesn't undo the
			// unsubscribes; just leave CategoryRemoved=false.
		}
	}

	writeData(w, http.StatusOK, result, nil)
}
