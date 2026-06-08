// Package ttrss imports Starred and Archived articles exported by Tiny Tiny
// RSS (the import_export plugin's XML format). All imported articles are
// landed in a single synthetic, non-polling feed and marked starred + read so
// they appear in the Starred view without re-subscribing the user to — or
// re-fetching — their original source feeds.
package ttrss

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// maxBytes caps the upload size. TT-RSS exports embed full article HTML, so
// they can be large; 50 MiB is generous. encoding/xml is XXE-safe by default,
// so size is the only memory vector.
const maxBytes = 50 << 20

// parkedNextFetch is a far-future timestamp (year ~2286) stored as the import
// feed's next_fetch so the poller's FeedsDue query (next_fetch <= now) never
// selects it. The feed has no fetchable URL anyway.
const parkedNextFetch int64 = 9_999_999_999

const importFeedTitle = "Imported (TT-RSS)"

// URLValidator guards outbound URLs (SSRF) for the live API pull. Return
// non-nil to reject. Nil means "accept all" — fine for the file-upload path,
// which never makes a network request.
type URLValidator func(ctx context.Context, raw string) error

// Service imports TT-RSS exports into a user's account.
type Service struct {
	Store *store.Store
	// ValidateURL guards the live-pull target (set from urlcheck.Check in
	// production). Nil accepts all — only safe for the file path.
	ValidateURL URLValidator
	// AllowPrivateURLs is passed to urlcheck.GuardedTransport so the DNS-
	// pinning transport respects the same private-IP opt-in as the rest of
	// the app (EMBER_ALLOW_PRIVATE_URLS).
	AllowPrivateURLs bool
	// HTTPClient overrides the default client used by the live pull (tests
	// inject httptest.Server.Client()). Nil builds a guarded 30s client.
	HTTPClient *http.Client
}

// NewService constructs a TT-RSS import service.
func NewService(s *store.Store) *Service {
	return &Service{Store: s}
}

// Result summarizes an import run.
type Result struct {
	Total         int `json:"total"`          // <article> elements seen
	Imported      int `json:"imported"`       // newly inserted (excludes duplicates)
	Skipped       int `json:"skipped"`        // unusable (no guid/link)
	Feeds         int `json:"feeds"`          // NEW subscriptions created (full-migrate API pull only)
	FeedsExisting int `json:"feeds_existing"` // feeds skipped because already subscribed
}

// article is one <article> node in the TT-RSS export. Unused fields
// (score, note, tag_cache, label_cache, published, feed_url, feed_title) are
// ignored by the decoder. CDATA-wrapped values decode into the string fields
// transparently.
type article struct {
	GUID    string `xml:"guid"`
	Title   string `xml:"title"`
	Content string `xml:"content"`
	Link    string `xml:"link"`
	Updated string `xml:"updated"`
}

// Import reads a TT-RSS export and stores every article in the user's
// single "Imported (TT-RSS)" feed, marking each starred + read. Articles are
// de-duplicated by UpsertArticle (guid, then content hash) so re-importing the
// same file is idempotent. Returns counts; a malformed document returns an
// error after partial progress.
func (s *Service) Import(ctx context.Context, userID int64, body io.Reader) (Result, error) {
	var res Result
	feedID, err := s.ensureImportFeed(ctx, userID)
	if err != nil {
		return res, err
	}

	dec := xml.NewDecoder(io.LimitReader(body, maxBytes))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, fmt.Errorf("ttrss: parse: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "article" {
			continue
		}
		var a article
		if err := dec.DecodeElement(&a, &se); err != nil {
			return res, fmt.Errorf("ttrss: decode article: %w", err)
		}
		res.Total++
		inserted, skipped, err := s.save(ctx, userID, feedID, normItem{
			guid:      a.GUID,
			link:      a.Link,
			title:     a.Title,
			content:   a.Content,
			published: parseTime(a.Updated),
		})
		if err != nil {
			return res, err
		}
		switch {
		case skipped:
			res.Skipped++
		case inserted:
			res.Imported++
		}
	}
	return res, nil
}

// normItem is a source-agnostic article (from the XML file or the live API)
// ready to be stored.
type normItem struct {
	guid, link, title, author, content string
	published                          int64
}

// save upserts one normalized item into the import feed and marks it
// starred + read (migrated history — avoids a large unread spike). Returns
// inserted (a new row was created) or skipped (no usable identifier).
// Idempotent on re-import via UpsertArticle's dedup.
func (s *Service) save(ctx context.Context, userID, feedID int64, it normItem) (inserted, skipped bool, err error) {
	guid := strings.TrimSpace(it.guid)
	if guid == "" {
		guid = strings.TrimSpace(it.link)
	}
	if guid == "" {
		return false, true, nil // nothing to identify or dedup on
	}
	link := feed.SafeHTTPURL(it.link)
	// Sanitize the embedded HTML before storing; imported bodies are rendered
	// via {@html} like any feed article. Derive plain text from the sanitized
	// body the same way feed.Parse does, so imported items get a card excerpt
	// and are full-text searchable. The hash is computed over the text (matching
	// the parser) for consistency; guid dedup still dominates, so re-imports
	// stay idempotent regardless.
	content := feed.SanitizeHTML(it.content)
	text := feed.HTMLToText(content)
	saved, ins, err := s.Store.UpsertArticle(ctx, models.Article{
		FeedID:      feedID,
		GUID:        guid,
		URL:         link,
		Title:       it.title,
		Author:      it.author,
		ContentHTML: content,
		ContentText: text,
		PublishedAt: it.published,
		ContentHash: feed.ContentHash(link, it.title, text),
	})
	if err != nil {
		return false, false, fmt.Errorf("ttrss: store article: %w", err)
	}
	if err := s.Store.SetStarred(ctx, userID, saved.ID, true); err != nil {
		return false, false, fmt.Errorf("ttrss: star: %w", err)
	}
	if err := s.Store.SetRead(ctx, userID, []int64{saved.ID}, true); err != nil {
		return false, false, fmt.Errorf("ttrss: mark read: %w", err)
	}
	// Imports never go through the poller's fetch path, so nothing enqueues
	// them for summarization. The SPA hides articles whose summary_model is
	// empty (OnlySummarized), so stamp not-yet-summarized ones 'skipped' —
	// they show immediately and don't sit forever in the pending-summary
	// count. Historical curated items don't need (and shouldn't bulk-hammer)
	// the LLM; the per-feed "Re-summarize" action remains available if wanted.
	// Guarding on empty summary_model (rather than `inserted`) means a
	// re-import also heals articles imported before this stamp existed, while
	// never clobbering a real summary an article already has from a live feed.
	if saved.SummaryModel == "" {
		if err := s.Store.UpdateSummary(ctx, saved.ID, "", "skipped"); err != nil {
			return false, false, fmt.Errorf("ttrss: mark summarized: %w", err)
		}
	}
	return ins, false, nil
}

// ensureImportFeed returns the id of the user's parked, non-polling import
// feed, creating it (and a non-muted subscription) on first call. Both calls
// are idempotent.
func (s *Service) ensureImportFeed(ctx context.Context, userID int64) (int64, error) {
	f, err := s.Store.UpsertFeed(ctx, models.Feed{
		URL:       fmt.Sprintf("ttrss-import://%d", userID),
		Title:     importFeedTitle,
		NextFetch: parkedNextFetch,
	})
	if err != nil {
		return 0, fmt.Errorf("ttrss: ensure feed: %w", err)
	}
	// Non-muted so the articles surface in the Starred smart view (which
	// excludes muted feeds). Subscribe is idempotent.
	if _, err := s.Store.Subscribe(ctx, models.Subscription{UserID: userID, FeedID: f.ID}); err != nil {
		return 0, fmt.Errorf("ttrss: subscribe: %w", err)
	}
	return f.ID, nil
}

// parseTime parses TT-RSS's "YYYY-MM-DD HH:MM:SS" updated stamp, falling back
// to RFC3339. Returns 0 (unknown) when unparseable.
func parseTime(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Unix()
		}
	}
	return 0
}
