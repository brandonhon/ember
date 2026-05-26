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

func (d *Dependencies) handleListStarterPacks(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, starterPacks, nil)
}

type starterImportResult struct {
	Pack        string `json:"pack"`
	CategoryID  int64  `json:"category_id"`
	FeedsAdded  int    `json:"feeds_added"`
	AlreadyHad  int    `json:"already_had"`
	FailedURLs  []string `json:"failed_urls,omitempty"`
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
