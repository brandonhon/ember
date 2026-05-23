package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// Test-mode constants. The e2e harness assumes these exact values.
const (
	testAdminUser     = "admin"
	testAdminPassword = "admintest"
	testFeedURL       = "https://example.test/feed"
	testFeedTitle     = "Example Tech Blog"
	testCategory      = "Tech"
)

// seedTestData is idempotent: it creates a known admin, a known feed, a
// category, and a fixed set of articles when the database is empty enough to
// matter. Tests rely on these exact strings.
func seedTestData(ctx context.Context, st *store.Store, a *auth.Auth, logger *slog.Logger) error {
	// Use a deterministic password so tests can log in.
	if _, _, err := a.BootstrapAdmin(ctx, testAdminUser, testAdminPassword); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	u, err := st.GetUserByUsername(ctx, testAdminUser)
	if err != nil {
		return fmt.Errorf("get admin: %w", err)
	}

	// Feed + category + subscription.
	feed, err := st.UpsertFeed(ctx, models.Feed{
		URL: testFeedURL, Title: testFeedTitle, SiteURL: "https://example.test",
	})
	if err != nil {
		return fmt.Errorf("upsert feed: %w", err)
	}

	var catID *int64
	cats, _ := st.ListCategories(ctx, u.ID)
	for i := range cats {
		if cats[i].Name == testCategory {
			catID = &cats[i].ID
			break
		}
	}
	if catID == nil {
		c, err := st.CreateCategory(ctx, models.Category{UserID: u.ID, Name: testCategory})
		if err != nil {
			return fmt.Errorf("create category: %w", err)
		}
		catID = &c.ID
	}
	if _, err := st.Subscribe(ctx, models.Subscription{
		UserID: u.ID, FeedID: feed.ID, CategoryID: catID,
	}); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	// Articles — deterministic GUIDs so reruns dedup cleanly. The harness
	// asserts the title of "first" appears in the list and the search hits
	// the unique word "espresso".
	type fix struct {
		guid, title, text, hash string
		published               int64
		summary                 string
	}
	now := a.Now()
	fixtures := []fix{
		{
			guid:      "fixture-1",
			title:     "First fixture article",
			text:      "This is the first article in the test fixture set.",
			hash:      "h-fixture-1",
			published: now.Unix() - 3600,
			summary:   "• Test summary point one\n• Test summary point two\n• Test summary point three",
		},
		{
			guid:      "fixture-2",
			title:     "Second fixture about espresso",
			text:      "How to brew espresso at home with a moka pot or a real machine.",
			hash:      "h-fixture-2",
			published: now.Unix() - 7200,
		},
		{
			guid:      "fixture-3",
			title:     "Third fixture article",
			text:      "An older article from earlier this week.",
			hash:      "h-fixture-3",
			published: now.Unix() - 86400*2,
		},
		{guid: "fixture-4", title: "Fourth fixture", text: "Lorem ipsum dolor sit amet.", hash: "h-fixture-4", published: now.Unix() - 86400*3},
		{guid: "fixture-5", title: "Fifth fixture", text: "Consectetur adipiscing elit.", hash: "h-fixture-5", published: now.Unix() - 86400*4},
		{guid: "fixture-6", title: "Sixth fixture", text: "Sed do eiusmod tempor incididunt.", hash: "h-fixture-6", published: now.Unix() - 86400*5},
		{guid: "fixture-7", title: "Seventh fixture", text: "Ut labore et dolore magna aliqua.", hash: "h-fixture-7", published: now.Unix() - 86400*6},
		{guid: "fixture-8", title: "Eighth fixture", text: "Ut enim ad minim veniam.", hash: "h-fixture-8", published: now.Unix() - 86400*7},
		{guid: "fixture-9", title: "Ninth fixture", text: "Quis nostrud exercitation ullamco.", hash: "h-fixture-9", published: now.Unix() - 86400*8},
		{guid: "fixture-10", title: "Tenth fixture", text: "Laboris nisi ut aliquip ex ea.", hash: "h-fixture-10", published: now.Unix() - 86400*9},
		{guid: "fixture-11", title: "Eleventh fixture", text: "Commodo consequat dolor in.", hash: "h-fixture-11", published: now.Unix() - 86400*10},
		{guid: "fixture-12", title: "Twelfth fixture", text: "Reprehenderit in voluptate velit.", hash: "h-fixture-12", published: now.Unix() - 86400*11},
	}
	for _, f := range fixtures {
		art, _, err := st.UpsertArticle(ctx, models.Article{
			FeedID:      feed.ID,
			GUID:        f.guid,
			URL:         "https://example.test/posts/" + f.guid,
			Title:       f.title,
			ContentHTML: "<p>" + f.text + "</p>",
			ContentText: f.text,
			ContentHash: f.hash,
			PublishedAt: f.published,
		})
		if err != nil {
			return fmt.Errorf("upsert fixture %s: %w", f.guid, err)
		}
		if f.summary != "" {
			_ = st.UpdateSummary(ctx, art.ID, f.summary, "noop")
		}
	}
	logger.Info("test mode: seeded admin + feed + fixtures",
		"user", testAdminUser, "feed_id", feed.ID, "category", testCategory)
	return nil
}
