package main

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// seedImages holds the per-topic thumbnail photos embedded at build time. They
// are emitted as base64 data: URIs by img() below — see the rationale there.
//
//go:embed seedimg/*.jpg
var seedImages embed.FS

// Test-mode constants. The e2e harness assumes these exact values.
const (
	testAdminUser     = "admin"
	testAdminPassword = "admintest"
	testFeedURL       = "https://example.test/feed"
	testFeedTitle     = "Example Tech Blog"
	testCategory      = "Technology"
)

// seedTestData is idempotent: it creates a known admin, then two layers of
// fixtures when the database is empty.
//
//  1. The e2e contract: feed id 1 ("Example Tech Blog") with article ids
//     1/2/3 — "First fixture article" (summary card), "Second fixture about
//     espresso" (search target), and a 2-day-old "Third fixture article"
//     (excluded from the Fresh view). The Playwright suite asserts these
//     exact strings, ids, and freshness, so do NOT reorder or rename them.
//  2. A realistic multi-feed/folder set with thumbnails and summaries,
//     stamped with recent timestamps so it sorts above the contract
//     fixtures. This is what the docs hero / marketing screenshots capture
//     (see web/scripts/screenshots.mjs and the dark-mode three-pane shots).
func seedTestData(ctx context.Context, st *store.Store, a *auth.Auth, logger *slog.Logger) error {
	if _, _, err := a.BootstrapAdmin(ctx, testAdminUser, testAdminPassword); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	u, err := st.GetUserByUsername(ctx, testAdminUser)
	if err != nil {
		return fmt.Errorf("get admin: %w", err)
	}

	// Folders (categories), created on demand and cached by name.
	catCache := map[string]*int64{}
	ensureCat := func(name string) (*int64, error) {
		if id, ok := catCache[name]; ok {
			return id, nil
		}
		cats, _ := st.ListCategories(ctx, u.ID)
		for i := range cats {
			if cats[i].Name == name {
				catCache[name] = &cats[i].ID
				return &cats[i].ID, nil
			}
		}
		c, err := st.CreateCategory(ctx, models.Category{UserID: u.ID, Name: name})
		if err != nil {
			return nil, fmt.Errorf("create category %q: %w", name, err)
		}
		catCache[name] = &c.ID
		return &c.ID, nil
	}

	subscribe := func(title, url, site, category string) (int64, error) {
		f, err := st.UpsertFeed(ctx, models.Feed{
			URL:        url,
			Title:      title,
			SiteURL:    site,
			FaviconURL: faviconFor(site),
		})
		if err != nil {
			return 0, fmt.Errorf("upsert feed %q: %w", title, err)
		}
		cid, err := ensureCat(category)
		if err != nil {
			return 0, err
		}
		if _, err := st.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID, CategoryID: cid}); err != nil {
			return 0, fmt.Errorf("subscribe %q: %w", title, err)
		}
		return f.ID, nil
	}

	now := a.Now()
	addArticle := func(feedID int64, guid string, art models.Article, summary string) error {
		art.FeedID = feedID
		art.GUID = guid
		if art.URL == "" {
			art.URL = "https://example.test/posts/" + guid
		}
		if art.ContentText == "" {
			art.ContentText = stripTags(art.ContentHTML)
		}
		art.ContentHash = "h-" + guid
		a, _, err := st.UpsertArticle(ctx, art)
		if err != nil {
			return fmt.Errorf("upsert article %q: %w", guid, err)
		}
		// Stamp every article so it clears the OnlySummarized UI gate: a real
		// summary renders the AI card, "skipped" shows the article without one.
		if summary != "" {
			_ = st.UpdateSummary(ctx, a.ID, summary, "noop")
		} else {
			_ = st.UpdateSummary(ctx, a.ID, "", "skipped")
		}
		return nil
	}

	// --- Layer 1: e2e contract (feed id 1 + article ids 1/2/3) ---
	exampleID, err := subscribe(testFeedTitle, testFeedURL, "https://example.test", testCategory)
	if err != nil {
		return err
	}
	if err := addArticle(exampleID, "fixture-1", models.Article{
		Title:       "First fixture article",
		ContentHTML: "<p>This is the first article in the test fixture set.</p>",
		PublishedAt: now.Unix() - 3600,
	}, "• Test summary point one\n• Test summary point two\n• Test summary point three"); err != nil {
		return err
	}
	if err := addArticle(exampleID, "fixture-2", models.Article{
		Title:       "Second fixture about espresso",
		ContentHTML: "<p>How to brew espresso at home with a moka pot or a real machine.</p>",
		PublishedAt: now.Unix() - 7200,
	}, ""); err != nil {
		return err
	}
	if err := addArticle(exampleID, "fixture-3", models.Article{
		Title:       "Third fixture article",
		ContentHTML: "<p>An older article from earlier this week.</p>",
		PublishedAt: now.Unix() - 86400*2,
	}, ""); err != nil {
		return err
	}

	// --- Layer 2: realistic feeds + stories for screenshots ---
	verge, err := subscribe("The Verge", "https://theverge.test/feed.xml", "https://www.theverge.com", "Technology")
	if err != nil {
		return err
	}
	ars, err := subscribe("Ars Technica", "https://arstechnica.test/feed.xml", "https://arstechnica.com", "Technology")
	if err != nil {
		return err
	}
	hn, err := subscribe("Hacker News", "https://hackernews.test/rss", "https://news.ycombinator.com", "Technology")
	if err != nil {
		return err
	}
	smashing, err := subscribe("Smashing Magazine", "https://smashingmag.test/feed", "https://www.smashingmagazine.com", "Design")
	if err != nil {
		return err
	}
	reuters, err := subscribe("Reuters World", "https://reutersworld.test/feed", "https://www.reuters.com", "World")
	if err != nil {
		return err
	}

	// Each thumbnail is a real photo embedded at build time (seedimg/<seed>.jpg)
	// and emitted as a base64 data: URI. Real http(s) image URLs would be
	// rewritten to the same-origin /api/img proxy, whose outbound fetch hangs in
	// CI and saturates the browser's 6-connections-per-origin pool, starving the
	// SPA's own API calls and timing out the whole e2e suite. data: URIs pass
	// through imageProxy.rewrite unchanged — a real picture renders, no fetch.
	img := func(seed string) string {
		b, err := seedImages.ReadFile("seedimg/" + seed + ".jpg")
		if err != nil {
			// Fallback keeps the layout if an asset is ever missing: 1x1 transparent PNG.
			return "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
		}
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
	}
	type story struct {
		feed                                int64
		title, author, image, summary, body string
		agoMin                              int64
	}
	stories := []story{
		{verge, "Apple unveils the M5 MacBook Pro with on-device AI cores", "Nilay Patel", img("macbook"),
			"Apple's new MacBook Pro leans hard into local inference, shipping a neural block that runs small language models entirely offline.\n\n• M5 adds a 32-core Neural Engine tuned for on-device LLMs\n• Battery life jumps to a claimed 24 hours of mixed use\n• Starts at $1,999; ships next month",
			"<p>Apple today announced the M5 MacBook Pro, a machine built around the idea that the most useful AI is the kind that never leaves your laptop. The headline feature is a redesigned Neural Engine capable of running 7B-parameter models locally at interactive speeds.</p><p>The company framed the launch around privacy: summaries, transcription, and search all happen on-device, with nothing sent to a server.</p>", 6},
		{ars, "SQLite turns 25: the little database that quietly runs the world", "Jim Salter", img("sqlite"),
			"A look back at how a public-domain embedded database became the most deployed SQL engine on the planet.\n\n• Ships in every phone, browser, and most apps\n• Single-file, zero-config, serverless by design\n• New JSONB and FTS5 work keep it modern",
			"<p>Twenty-five years after its first commit, SQLite is everywhere — and almost invisible. This piece traces its design philosophy and why \"just a file\" turned out to be a superpower.</p>", 18},
		{hn, "Show HN: I built a self-hosted RSS reader with on-device summaries", "throwaway42", img("reader"), "",
			"<p>I got tired of cloud readers mining my reading habits, so I built my own: a single Go binary with embedded SQLite, an embedded Svelte SPA, and an optional local LLM for summaries. No cloud, no tracking, a paper-and-ink UI.</p>" +
				"<p>The whole thing runs from one ~25 MB binary behind any reverse proxy. Feeds refresh on a 15-second background poll that prepends new articles without a reload, and a small favicon dot flags unread items. Full-text search is SQLite FTS5; there are folders, saved searches, per-article tags, and a rules engine.</p>" +
				"<p>The AI is fully optional — point it at a local Ollama model and each article gets a paragraph-plus-bullets summary that never leaves your box, or turn it off entirely and the reader works exactly the same. I'd love feedback on the architecture and the threat model.</p>", 32},
		{smashing, "Designing calm interfaces: the case for paper-and-ink palettes", "Vitaly Friedman", img("design"),
			"Why warm, low-contrast palettes reduce reading fatigue and how to build one with CSS color-mix().\n\n• Warm neutrals beat pure black/white for long reads\n• color-mix() derives a full palette from three anchors\n• Respect prefers-color-scheme and prefers-contrast",
			"<p>The default web is a glaring white rectangle. This article argues for a softer, editorial aesthetic and walks through deriving a themeable palette from a handful of base colors.</p>", 51},
		{reuters, "Undersea cable consortium announces new trans-Pacific route", "Reuters Staff", img("cable"), "",
			"<p>A group of carriers will lay a new high-capacity fiber route across the Pacific, aiming to cut latency between Asia and North America.</p>", 74},
		{verge, "Framework's modular laptop gets a mainboard upgrade", "Sean Hollister", img("framework"), "",
			"<p>The repairable laptop keeps its promise: a drop-in mainboard breathes new life into three-year-old chassis.</p>", 95},
		{ars, "Linux 6.18 lands with a major scheduler rework", "Jonathan Corbet", img("linux"), "",
			"<p>The new release focuses on latency under heavy load and better handling of hybrid CPU topologies.</p>", 140},
		{hn, "Ask HN: What's your homelab backup strategy in 2026?", "ops_nerd", img("homelab"), "",
			"<p>A long thread on 3-2-1 backups, ZFS snapshots, and off-site replication for self-hosters.</p>", 190},
		{smashing, "A practical guide to container queries", "Rachel Andrew", img("css"), "",
			"<p>Container queries finally let components be responsive to their context, not just the viewport.</p>", 260},
		{reuters, "Renewables overtake coal in the global electricity mix", "Reuters Staff", img("solar"), "",
			"<p>For the first time, wind and solar generated more power than coal over a full quarter, a new report finds.</p>", 360},
		{verge, "The best e-ink tablets for reading and note-taking", "Dan Seifert", img("eink"), "",
			"<p>Our roundup of the paper-like tablets worth your money this year.</p>", 540},
		{ars, "Inside the open-source push to replace the password", "Dan Goodin", img("passkey"), "",
			"<p>Passkeys are spreading fast. We look at the FIDO2 stack and what self-hosters can do today.</p>", 800},
	}
	for i, s := range stories {
		if err := addArticle(s.feed, fmt.Sprintf("hero-%d", i), models.Article{
			Title:       s.title,
			Author:      s.author,
			ImageURL:    s.image,
			ContentHTML: s.body,
			PublishedAt: now.Unix() - s.agoMin*60,
		}, s.summary); err != nil {
			return err
		}
	}

	// --- Layer 3: cross-feed dedup demo ----------------------------------
	// Two scenarios so the "Also in N feeds" pill is visible end-to-end:
	//
	//   1. Wire story: same headline syndicated by Reuters and The Verge
	//      within the 48h window. Title-fingerprint clustering collapses
	//      them to one card; clicking the pill expands the sibling feed.
	//
	//   2. Tracking-param variants: HN linking to a Smashing piece — same
	//      canonical URL (smashingmagazine.com/.../calm-web) with different
	//      ?utm_*= query strings. CanonicalURL strips the trackers, so the
	//      two rows share a cluster_id and collapse to one.
	syndicated := []struct {
		feed                                     int64
		guid, title, author, url, image, summary string
		body                                     string
		agoMin                                   int64
	}{
		// Wire story pair — different URLs, same fingerprint, both Fresh.
		{reuters, "wire-openai-compute-1",
			"OpenAI signs $50B compute deal with chipmaker",
			"Reuters Staff",
			"https://reutersworld.test/openai-compute-deal",
			img("compute"),
			"A five-year capacity guarantee underscores AI's silicon appetite.\n\n• Multi-year, multi-country commitment\n• Custom accelerator silicon\n• On-prem buildouts in 4 countries",
			"<p>OpenAI and a major fab-light chipmaker have signed a five-year, ~$50B capacity deal, a sign that hyperscaler appetite for guaranteed silicon supply continues to outpace the industry's ability to manufacture it.</p>", 8},
		{verge, "wire-openai-compute-2",
			"OpenAI Signs $50B Compute Deal With Chipmaker",
			"Alex Heath",
			"https://theverge.test/openai-50b-compute",
			img("compute2"), "",
			"<p>The Verge confirms the multi-year capacity deal with new details on which fabs are involved and how OpenAI plans to deploy the silicon across four regions.</p>", 14},

		// Tracking-param pair — same canonical_url, distinct raw URLs.
		{smashing, "track-calm-web-1",
			"How to ship a fast, calm website in 2026",
			"Adam Wathan",
			"https://smashingmag.test/2026/calm-web?utm_source=newsletter&utm_medium=email",
			img("perf"),
			"Performance budgets, prefetch hints, and minimal JS.\n\n• Budget every kilobyte\n• Prefetch the next likely page\n• Default to no-JS until proven otherwise",
			"<p>The web isn't slow because we don't know how to make it fast; it's slow because we don't budget for it. This post walks through a working perf budget and the small handful of techniques that move the needle in 2026.</p>", 22},
		{hn, "track-calm-web-2",
			"How to ship a fast, calm website in 2026",
			"perf_curious",
			"https://smashingmag.test/2026/calm-web?utm_source=hackernews&fbclid=xyz789",
			img("perf"), "",
			"<p>(link points at smashingmagazine.com — HN comment thread linked off this post.)</p>", 28},
	}
	for _, s := range syndicated {
		if err := addArticle(s.feed, s.guid, models.Article{
			Title:       s.title,
			Author:      s.author,
			URL:         s.url,
			ImageURL:    s.image,
			ContentHTML: s.body,
			PublishedAt: now.Unix() - s.agoMin*60,
		}, s.summary); err != nil {
			return err
		}
	}

	logger.Info("test mode: seeded fixtures",
		"user", testAdminUser, "feeds", 6, "articles", 3+len(stories)+len(syndicated))
	return nil
}

// faviconFor returns a DuckDuckGo favicon URL for a feed's site, used only to
// give the seeded feeds recognizable chips in screenshots.
func faviconFor(site string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(site, "https://"), "http://")
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return ""
	}
	return "https://icons.duckduckgo.com/ip3/" + host + ".ico"
}

// stripTags returns a rough plain-text rendering of an HTML fragment for the
// article's content_text (search/excerpt) field.
func stripTags(s string) string {
	out := s
	for {
		i := strings.IndexByte(out, '<')
		if i < 0 {
			break
		}
		j := strings.IndexByte(out[i:], '>')
		if j < 0 {
			break
		}
		out = out[:i] + " " + out[i+j+1:]
	}
	return strings.TrimSpace(strings.Join(strings.Fields(out), " "))
}
