package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/config"
	"github.com/brandonhon/ember/internal/db"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// runSeedFull populates a database with a broad, idempotent-on-fresh fixture set
// that exercises every user-facing feature — multiple users, folders, feeds in
// varied states, articles across every time bucket (fresh / today / this-week /
// prune-eligible), read+star+later+shared state, cross-feed dedup, filters (one
// per action), boards, tags, saved searches, an email inbox, and non-default
// admin settings. It is the `ember seed` subcommand, invoked by `make sandbox`
// against a throwaway compose stack. Distinct from the minimal EMBER_TEST_MODE
// seed (the frozen e2e contract) — do not merge the two.
func runSeedFull() {
	// Wrapper so the actual work can defer dbh.Close() and return errors —
	// os.Exit lives only here, where no defer is pending (gocritic exitAfterDefer).
	if err := seedFullMain(); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}
}

func seedFullMain() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx := context.Background()
	dbh, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("db open: %w", err)
	}
	defer dbh.Close()
	st := store.New(dbh)

	sessionKey := cfg.SessionKey
	if sessionKey == "" {
		sessionKey = "00000000000000000000000000000000-ember-test-mode-key"
	}
	a, err := auth.New(st, sessionKey)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	return seedFull(ctx, st, a, cfg, logger)
}

func seedFull(ctx context.Context, st *store.Store, a *auth.Auth, cfg config.Config, logger *slog.Logger) error {
	now := time.Now()
	// Time buckets relative to now, so the windowed views are all demonstrable:
	//   fresh        — inside the 6h Fresh window
	//   today        — inside the 24h reading window, outside Fresh
	//   thisWeek     — inside the 1-week retention/search window, outside reading
	//   pruneOld     — older than retention → the daily prune sweep removes it
	ago := func(d time.Duration) int64 { return now.Add(-d).Unix() }
	fresh, today, thisWeek, pruneOld := ago(2*time.Hour), ago(10*time.Hour), ago(3*24*time.Hour), ago(9*24*time.Hour)

	// --- Users: admin (from config, bootstrapped by the server already) + a
	// second user so sharing and multi-user views have someone to point at.
	adminName := cfg.AdminUser
	if adminName == "" {
		adminName = "admin"
	}
	if _, _, err := a.BootstrapAdmin(ctx, adminName, firstNonEmpty(cfg.AdminPassword, "admintest")); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	admin, err := st.GetUserByUsername(ctx, adminName)
	if err != nil {
		return fmt.Errorf("get admin: %w", err)
	}
	readerHash, err := a.HashPassword("readerpass")
	if err != nil {
		return fmt.Errorf("hash reader pw: %w", err)
	}
	reader, err := st.CreateUser(ctx, models.User{Username: "reader", PasswordHash: readerHash})
	if err != nil {
		return fmt.Errorf("create reader: %w", err)
	}

	// --- Folders (with colors + positions). ---
	type cat struct {
		name, color string
		pos         int
	}
	catID := map[string]*int64{}
	for _, c := range []cat{
		{"Technology", "#3b82f6", 0}, {"Design", "#8b5cf6", 1},
		{"World", "#10b981", 2}, {"News", "#f59e0b", 3},
	} {
		created, err := st.CreateCategory(ctx, models.Category{
			UserID: admin.ID, Name: c.name, Color: c.color, Position: c.pos,
		})
		if err != nil {
			return fmt.Errorf("category %q: %w", c.name, err)
		}
		id := created.ID
		catID[c.name] = &id
	}

	// --- Feeds + admin subscriptions (one errored feed, one muted sub). ---
	feedID := map[string]int64{}
	subID := map[string]int64{}
	subscribe := func(key, title, url, site, category string, errCount int, muted bool) error {
		f, err := st.UpsertFeed(ctx, models.Feed{
			URL: url, Title: title, SiteURL: site, FaviconURL: faviconFor(site),
			ErrorCount: errCount,
			LastError:  ternary(errCount > 0, "dial tcp: connection refused", ""),
		})
		if err != nil {
			return fmt.Errorf("feed %q: %w", title, err)
		}
		feedID[key] = f.ID
		sub, err := st.Subscribe(ctx, models.Subscription{UserID: admin.ID, FeedID: f.ID, CategoryID: catID[category]})
		if err != nil {
			return fmt.Errorf("subscribe %q: %w", title, err)
		}
		subID[key] = sub.ID
		if muted {
			m := true
			if err := st.UpdateSubscription(ctx, admin.ID, sub.ID, store.UpdateSubscriptionPatch{Muted: &m}); err != nil {
				return fmt.Errorf("mute %q: %w", title, err)
			}
		}
		return nil
	}
	feeds := []struct {
		key, title, url, site, cat string
		errCount                   int
		muted                      bool
	}{
		{"verge", "The Verge", "https://theverge.test/feed.xml", "https://www.theverge.com", "Technology", 0, false},
		{"ars", "Ars Technica", "https://arstechnica.test/feed.xml", "https://arstechnica.com", "Technology", 0, false},
		{"smashing", "Smashing Magazine", "https://smashingmag.test/feed", "https://www.smashingmagazine.com", "Design", 0, false},
		{"reuters", "Reuters World", "https://reutersworld.test/feed", "https://www.reuters.com", "World", 0, false},
		{"flaky", "Flaky Feed", "https://flaky.test/feed", "https://flaky.test", "News", 5, false},
		{"noisy", "Noisy Newswire", "https://noisy.test/feed", "https://noisy.test", "News", 0, true},
		{"firehose", "Firehose Daily", "https://firehose.test/feed", "https://firehose.test", "News", 0, false},
	}
	for _, f := range feeds {
		if err := subscribe(f.key, f.title, f.url, f.site, f.cat, f.errCount, f.muted); err != nil {
			return err
		}
	}
	// Reader subscribes to a couple so multi-user + sharing has a real target.
	for _, k := range []string{"verge", "reuters"} {
		if _, err := st.Subscribe(ctx, models.Subscription{UserID: reader.ID, FeedID: feedID[k]}); err != nil {
			return fmt.Errorf("reader subscribe %s: %w", k, err)
		}
	}

	// --- Articles across every time bucket, stamped so they clear the summary
	// gate. Returns the new article id so callers can attach state. ---
	artN := 0
	addArticle := func(feed string, title, body, summary string, pub int64) (int64, error) {
		artN++
		guid := fmt.Sprintf("full-%d", artN)
		art := models.Article{
			FeedID: feedID[feed], GUID: guid, Title: title,
			URL: "https://example.test/posts/" + guid, ContentHTML: body,
			ContentText: stripTags(body), ContentHash: "h-" + guid, PublishedAt: pub,
		}
		saved, _, err := st.UpsertArticle(ctx, art)
		if err != nil {
			return 0, fmt.Errorf("article %q: %w", title, err)
		}
		if summary != "" {
			_ = st.UpdateSummary(ctx, saved.ID, summary, "noop")
		} else {
			_ = st.UpdateSummary(ctx, saved.ID, "", "skipped")
		}
		return saved.ID, nil
	}

	mk := []struct {
		feed, title, body, summary string
		pub                        int64
	}{
		{"verge", "Apple unveils the M5 MacBook Pro with on-device AI cores",
			"<p>The headline feature is a redesigned Neural Engine capable of running 7B-parameter models locally.</p>",
			"• 32-core Neural Engine tuned for on-device LLMs\n• 24-hour battery\n• Starts at $1,999", fresh},
		{"ars", "SQLite turns 25: the little database that quietly runs the world",
			"<p>Twenty-five years after its first commit, SQLite is everywhere — and almost invisible.</p>",
			"• Ships in every phone and browser\n• Single-file, serverless\n• JSONB + FTS5 keep it modern", fresh},
		{"smashing", "Designing calm interfaces: the case for paper-and-ink palettes",
			"<p>Why warm, low-contrast palettes reduce reading fatigue.</p>", "", today},
		{"reuters", "Renewables overtake coal in the global electricity mix",
			"<p>Wind and solar generated more power than coal over a full quarter.</p>",
			"• A global first over a full quarter\n• Driven by solar buildout", today},
		{"verge", "Framework's modular laptop gets a mainboard upgrade",
			"<p>A drop-in mainboard breathes new life into three-year-old chassis.</p>", "", thisWeek},
		{"ars", "Linux 6.18 lands with a major scheduler rework",
			"<p>The release focuses on latency under heavy load.</p>", "", thisWeek},
		{"flaky", "This feed has been erroring for a while",
			"<p>The sidebar shows its error indicator; the article still reads fine.</p>", "", today},
		{"noisy", "You should not see this counted (muted)",
			"<p>Muted subscriptions stay out of the unread badges.</p>", "", fresh},
		{"reuters", "An older wire story past the retention window",
			"<p>Older than a week — the daily retention sweep will prune it unless saved.</p>", "", pruneOld},
	}
	artID := make([]int64, 0, len(mk))
	for _, m := range mk {
		id, err := addArticle(m.feed, m.title, m.body, m.summary, m.pub)
		if err != nil {
			return err
		}
		artID = append(artID, id)
	}

	// --- Bulk volume so paging + mark-all-read are exercisable. The "Firehose"
	// feed gets 64 recent unread articles (>50 → Load more in the feed view AND
	// in Fresh / Today / All Unread); the shared "update"/"AI" keywords make
	// search paginate too (>25 hits). 30 more spread across the other feeds
	// inside the 48h reading window. Every bulk item shares the same word pool
	// so a search like "update" returns far more than one page.
	bulkIDs := make([]int64, 0, 96)
	for i := 0; i < 64; i++ {
		// Stagger within the last ~5h so they all land in the 6h Fresh window.
		pub := now.Add(-time.Duration(i*4) * time.Minute).Unix()
		summary := ""
		if i%3 == 0 {
			summary = "• Auto-generated digest point\n• Second point\n• Third point"
		}
		id, err := addArticle("firehose",
			fmt.Sprintf("Firehose update #%02d — release notes and AI roundup", i+1),
			fmt.Sprintf("<p>Item %d in the firehose, mentioning AI, release, and update for search testing.</p>", i+1),
			summary, pub)
		if err != nil {
			return err
		}
		bulkIDs = append(bulkIDs, id)
	}
	spread := []string{"verge", "ars", "smashing", "reuters"}
	for i := 0; i < 30; i++ {
		pub := now.Add(-time.Duration(2+i) * time.Hour).Unix() // 2h..31h ago
		f := spread[i%len(spread)]
		id, err := addArticle(f,
			fmt.Sprintf("%s briefing %d: an AI + design + world-news update", f, i+1),
			fmt.Sprintf("<p>Briefing %d with an AI update and a release mention.</p>", i+1),
			"", pub)
		if err != nil {
			return err
		}
		bulkIDs = append(bulkIDs, id)
	}

	// --- Pending-summary tail: 10 firehose items left UNSUMMARIZED on purpose.
	// With AI on they're hidden by the summary gate until the Ollama worker
	// stamps them — which is what drives the sidebar "Summarizing N…" indicator
	// and proves the gate. (Stamped bulk above stays visible for paging.)
	for i := 0; i < 10; i++ {
		artN++
		guid := fmt.Sprintf("pending-%d", artN)
		if _, _, err := st.UpsertArticle(ctx, models.Article{
			FeedID: feedID["firehose"], GUID: guid,
			Title:       fmt.Sprintf("Pending summary #%d (awaiting the AI worker)", i+1),
			URL:         "https://example.test/posts/" + guid,
			ContentHTML: "<p>Unsummarized on purpose — hidden until the summarizer stamps it.</p>",
			ContentText: "Unsummarized pending article.",
			ContentHash: "h-" + guid, PublishedAt: now.Add(-time.Duration(i) * time.Minute).Unix(),
		}); err != nil {
			return fmt.Errorf("pending article: %w", err)
		}
	}

	// --- Cross-feed dedup: same headline syndicated by Reuters + The Verge in
	// the Fresh window → collapses to one card with the "Also in 2" pill. ---
	for _, d := range []struct{ feed, title, url string }{
		{"reuters", "OpenAI signs $50B compute deal with chipmaker", "https://reutersworld.test/openai-compute-deal"},
		{"verge", "OpenAI Signs $50B Compute Deal With Chipmaker", "https://theverge.test/openai-50b-compute"},
	} {
		artN++
		guid := fmt.Sprintf("dup-%d", artN)
		saved, _, err := st.UpsertArticle(ctx, models.Article{
			FeedID: feedID[d.feed], GUID: guid, Title: d.title, URL: d.url,
			ContentHTML: "<p>Syndicated wire story.</p>", ContentText: "Syndicated wire story.",
			ContentHash: "h-" + guid, PublishedAt: ago(3 * time.Hour),
		})
		if err != nil {
			return fmt.Errorf("dup article: %w", err)
		}
		_ = st.UpdateSummary(ctx, saved.ID, "", "skipped")
	}

	// --- Per-user state: read / starred / read-later / shared. The bulk pool
	// feeds these so mark-all-read leaves plenty unread behind Load more, and
	// reading stats have read history. (~15 read, ~14 starred, ~14 later.)
	_ = st.SetRead(ctx, admin.ID, []int64{artID[4], artID[5]}, true)
	for i := 0; i < 13 && i < len(bulkIDs); i++ {
		_ = st.SetRead(ctx, admin.ID, []int64{bulkIDs[i]}, true)
	}
	_ = st.SetStarred(ctx, admin.ID, artID[0], true)
	_ = st.SetStarred(ctx, admin.ID, artID[3], true)
	for i := 20; i < 32 && i < len(bulkIDs); i++ {
		_ = st.SetStarred(ctx, admin.ID, bulkIDs[i], true)
	}
	_ = st.SetLater(ctx, admin.ID, artID[1], true)
	_ = st.SetLater(ctx, admin.ID, artID[2], true)
	for i := 40; i < 52 && i < len(bulkIDs); i++ {
		_ = st.SetLater(ctx, admin.ID, bulkIDs[i], true)
	}
	if _, err := st.CreateShare(ctx, models.Share{
		ArticleID: artID[0], FromUser: admin.ID, ToUser: reader.ID, Note: "Thought you'd like this",
	}); err != nil {
		return fmt.Errorf("share: %w", err)
	}

	// --- Tags (drives the tag chips + per-tag view). ---
	for _, t := range []struct {
		id  int64
		tag string
	}{{artID[0], "ai"}, {artID[0], "apple"}, {artID[1], "databases"}, {artID[3], "energy"}} {
		if err := st.AddArticleTag(ctx, admin.ID, t.id, t.tag); err != nil {
			return fmt.Errorf("tag: %w", err)
		}
	}

	// --- A board + pinned articles. ---
	board, err := st.CreateBoard(ctx, models.Board{UserID: admin.ID, Name: "Deep Dives"})
	if err != nil {
		return fmt.Errorf("board: %w", err)
	}
	for _, id := range []int64{artID[1], artID[3]} {
		if err := st.AddArticleToBoard(ctx, admin.ID, board.ID, id); err != nil {
			return fmt.Errorf("board add: %w", err)
		}
	}

	// --- Filters: one per action type (the engine runs these at ingest). ---
	filters := []models.Filter{
		{Name: "Mute crypto noise", MatchJSON: `{"field":"title","op":"contains","value":"crypto"}`, Action: "mark_read", Priority: 100, Enabled: true},
		{Name: "Star Apple news", MatchJSON: `{"field":"title","op":"contains","value":"Apple"}`, Action: "star", Priority: 90, Enabled: true},
		{Name: "Hide press releases", MatchJSON: `{"field":"author","op":"equals","value":"PR Newswire"}`, Action: "hide", Priority: 110, Enabled: false},
		{Name: "Tag AI stories", MatchJSON: `{"field":"content","op":"contains","value":"AI"}`, Action: "tag", ActionValue: "ai", Priority: 80, Enabled: true},
		{Name: "Board the deep dives", MatchJSON: `{"field":"title","op":"contains","value":"scheduler"}`, Action: "add_to_board", ActionValue: fmt.Sprintf("%d", board.ID), Priority: 70, Enabled: true},
	}
	for _, f := range filters {
		f.UserID = admin.ID
		if _, err := st.CreateFilter(ctx, f); err != nil {
			return fmt.Errorf("filter %q: %w", f.Name, err)
		}
	}

	// --- Saved searches. ---
	for _, ss := range []models.SavedSearch{
		{Name: "AI coverage", Query: "AI"}, {Name: "Renewables", Query: "renewables"},
	} {
		ss.UserID = admin.ID
		if _, err := st.CreateSavedSearch(ctx, ss); err != nil {
			return fmt.Errorf("saved search %q: %w", ss.Name, err)
		}
	}

	// --- Email inbox (per-user newsletter address). ---
	if _, err := st.EnsureInbox(ctx, admin.ID); err != nil {
		return fmt.Errorf("inbox: %w", err)
	}

	// --- Non-default admin settings, to prove the options persist. ---
	_ = st.PutReadingWindowHours(ctx, 48)
	_ = st.PutSearchWindowHours(ctx, 72)
	_ = st.PutPollMinInterval(ctx, 15*time.Minute)
	_ = st.PutAppSetting(ctx, "branding_name", "Ember Sandbox")

	logger.Info("full seed complete",
		"admin", adminName, "second_user", "reader", "feeds", len(feeds),
		"articles", artN, "filters", len(filters), "board_id", board.ID)
	fmt.Printf("seeded: admin=%s/%s  reader=reader/readerpass  feeds=%d articles=%d\n",
		adminName, firstNonEmpty(cfg.AdminPassword, "admintest"), len(feeds), artN)
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
