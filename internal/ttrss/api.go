package ttrss

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/urlcheck"
)

// TT-RSS virtual feed IDs (see API reference): -1 = Starred, 0 = Archived.
const (
	feedStarred  = -1
	feedArchived = 0
)

// getFeeds cat_id sentinel: -4 returns every subscribed feed regardless of
// category. Each returned feed still carries its real cat_id for folder mapping.
const feedsCatAll = -4

const (
	headlineLimit  = 200      // page size for getHeadlines
	feedsLimit     = 200      // page size for getFeeds
	maxArticles    = 100_000  // safety cap on a single pull
	maxFeeds       = 10_000   // safety cap on a single subscription enumeration
	maxAPIResponse = 16 << 20 // cap per API response body
	apiCallTimeout = 30 * time.Second
)

// APIOptions configures a live pull from a running TT-RSS instance.
type APIOptions struct {
	BaseURL        string
	Username       string
	Password       string
	ImportFeeds    bool // migrate subscriptions (getFeeds) + recreate categories
	ImportStarred  bool
	ImportArchived bool
}

// ImportFromAPI logs into a running TT-RSS instance and migrates the user's
// account: when ImportFeeds is set it re-subscribes them to every TT-RSS feed
// (recreating categories as folders); when ImportStarred/ImportArchived are set
// it pulls those articles via getHeadlines into the parked import feed (starred
// + read), as the file path does. Credentials are used only for this call and
// never persisted.
//
// Note: the TT-RSS JSON API is disabled by default — the source user must
// enable "API access" in their TT-RSS preferences first.
func (s *Service) ImportFromAPI(ctx context.Context, userID int64, opt APIOptions) (Result, error) {
	var res Result
	if !opt.ImportFeeds && !opt.ImportStarred && !opt.ImportArchived {
		return res, errors.New("ttrss: nothing selected to import")
	}
	endpoint := apiEndpoint(opt.BaseURL)
	if s.ValidateURL != nil {
		if err := s.ValidateURL(ctx, endpoint); err != nil {
			return res, fmt.Errorf("ttrss: URL rejected: %w", err)
		}
	}
	client := s.apiClient(ctx)

	sid, err := s.login(ctx, client, endpoint, opt.Username, opt.Password)
	if err != nil {
		return res, err
	}
	defer s.logout(ctx, client, endpoint, sid) // best effort

	// Subscriptions first so the user's feed list/folders are in place before
	// the (potentially long) article pull.
	if opt.ImportFeeds {
		if err := s.importSubscriptions(ctx, client, endpoint, sid, userID, &res); err != nil {
			return res, err
		}
	}

	if opt.ImportStarred || opt.ImportArchived {
		feedID, err := s.ensureImportFeed(ctx, userID)
		if err != nil {
			return res, err
		}
		var feeds []int
		if opt.ImportStarred {
			feeds = append(feeds, feedStarred)
		}
		if opt.ImportArchived {
			feeds = append(feeds, feedArchived)
		}
		for _, fid := range feeds {
			if err := s.pull(ctx, client, endpoint, sid, fid, userID, feedID, &res); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// importSubscriptions enumerates the user's TT-RSS feeds (getFeeds, all
// categories) and subscribes them in ember, recreating TT-RSS categories as
// ember folders. Feeds the user is already subscribed to are skipped (counted
// in res.FeedsExisting) and left untouched — re-running a migration never
// re-adds or re-files an existing feed. New feeds land with next_fetch=0 so the
// poller backfills their articles on its next tick — no inline fetch here,
// which keeps a several-hundred-feed migration fast. SSRF-blocked feed URLs are
// skipped (non-fatal), matching OPML import. res.Feeds counts NEW subscriptions.
func (s *Service) importSubscriptions(ctx context.Context, client *http.Client, endpoint, sid string, userID int64, res *Result) error {
	cats, err := s.getCategories(ctx, client, endpoint, sid)
	if err != nil {
		return err
	}
	catNames := make(map[int]string, len(cats))
	for _, c := range cats {
		catNames[int(c.ID)] = c.Title
	}
	emberCat := make(map[int]*int64) // TT-RSS cat id -> ember category id (cached)

	// Snapshot the feeds the user is already subscribed to so we skip them
	// rather than re-subscribe (and re-count) — same dedup pattern as the
	// starter-pack import.
	existing, err := s.Store.ListFeedsForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("ttrss: list existing feeds: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, f := range existing {
		have[f.URL] = true
	}

	offset := 0
	for offset < maxFeeds {
		feeds, err := s.getFeeds(ctx, client, endpoint, sid, offset)
		if err != nil {
			return err
		}
		if len(feeds) == 0 {
			break
		}
		for _, f := range feeds {
			url := strings.TrimSpace(f.FeedURL)
			if url == "" {
				continue // virtual/special feed (no real source URL)
			}
			if have[url] {
				res.FeedsExisting++
				continue // already subscribed — don't add again
			}
			if s.ValidateURL != nil {
				if err := s.ValidateURL(ctx, url); err != nil {
					continue // SSRF-blocked — skip, don't abort the migration
				}
			}
			catID, err := s.resolveCategory(ctx, userID, int(f.CatID), catNames, emberCat)
			if err != nil {
				return err
			}
			title := strings.TrimSpace(f.Title)
			if title == "" {
				title = url
			}
			fd, err := s.Store.UpsertFeed(ctx, models.Feed{URL: url, Title: title})
			if err != nil {
				return fmt.Errorf("ttrss: ensure subscribed feed: %w", err)
			}
			if _, err := s.Store.Subscribe(ctx, models.Subscription{
				UserID: userID, FeedID: fd.ID, CategoryID: catID,
			}); err != nil {
				return fmt.Errorf("ttrss: subscribe: %w", err)
			}
			have[url] = true // guard against duplicate URLs across pages
			res.Feeds++
		}
		offset += len(feeds)
		if len(feeds) < feedsLimit {
			break // last page
		}
	}
	return nil
}

// resolveCategory maps a TT-RSS category id to an ember category id, creating
// the ember category on first use and caching the result. Returns nil for
// uncategorized (TT-RSS cat 0), virtual categories (negative), or any category
// with no usable name — those feeds land uncategorized.
func (s *Service) resolveCategory(ctx context.Context, userID int64, ttCatID int, catNames map[int]string, emberCat map[int]*int64) (*int64, error) {
	if id, ok := emberCat[ttCatID]; ok {
		return id, nil
	}
	name := strings.TrimSpace(catNames[ttCatID])
	if ttCatID <= 0 || name == "" || strings.EqualFold(name, "Uncategorized") {
		emberCat[ttCatID] = nil
		return nil, nil
	}
	c, err := s.Store.CreateCategory(ctx, models.Category{UserID: userID, Name: name})
	switch {
	case errors.Is(err, store.ErrConflict):
		existing, lerr := s.Store.ListCategories(ctx, userID)
		if lerr != nil {
			return nil, lerr
		}
		for i := range existing {
			if existing[i].Name == name {
				emberCat[ttCatID] = &existing[i].ID
				return &existing[i].ID, nil
			}
		}
		emberCat[ttCatID] = nil // conflict but not found — treat as uncategorized
		return nil, nil
	case err != nil:
		return nil, err
	default:
		emberCat[ttCatID] = &c.ID
		return &c.ID, nil
	}
}

// apiEndpoint normalizes a user-entered base URL to the TT-RSS API endpoint
// (<base>/api/), prepending https:// when no scheme is given.
func apiEndpoint(base string) string {
	base = feed.NormalizeInputURL(base)
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/api") {
		return base + "/"
	}
	return base + "/api/"
}

// apiClient builds the HTTP client for the live pull. ctx is the import
// request's context, threaded into the redirect SSRF guard so a slow redirect
// check honors the caller's cancellation/deadline rather than running detached.
func (s *Service) apiClient(ctx context.Context) *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	c := &http.Client{
		Timeout:   apiCallTimeout,
		Transport: urlcheck.GuardedTransport(s.AllowPrivateURLs),
	}
	if s.ValidateURL != nil {
		validate := s.ValidateURL
		c.CheckRedirect = feed.RedirectGuard(func(raw string) error {
			// Use a short detached context so a cancelled import request doesn't
			// produce misleading "context cancelled" SSRF-rejection log lines.
			rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return validate(rctx, raw)
		})
	} else {
		// Fail-safe: block all redirects when no validator is configured rather
		// than forwarding them unchecked. ValidateURL should always be set in
		// production; this prevents a misconfigured zero-value Service from
		// silently opening an SSRF path via redirect chains.
		c.CheckRedirect = func(*http.Request, []*http.Request) error {
			return errors.New("ttrss: redirect blocked — ValidateURL not configured")
		}
	}
	return c
}

// pull paginates getHeadlines for one virtual feed, saving each article.
func (s *Service) pull(ctx context.Context, client *http.Client, endpoint, sid string, feedID int, userID, importFeedID int64, res *Result) error {
	skip := 0
	for skip < maxArticles {
		items, err := s.getHeadlines(ctx, client, endpoint, sid, feedID, skip)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			break
		}
		for _, h := range items {
			res.Total++
			inserted, skipped, err := s.save(ctx, userID, importFeedID, normItem{
				guid:      h.GUID,
				link:      feed.SafeHTTPURL(h.Link),
				title:     h.Title,
				author:    h.Author,
				content:   h.Content,
				published: h.Updated,
			})
			if err != nil {
				return err
			}
			switch {
			case skipped:
				res.Skipped++
			case inserted:
				res.Imported++
			}
		}
		skip += len(items)
		if len(items) < headlineLimit {
			break // last page
		}
	}
	return nil
}

// --- API wire types -------------------------------------------------------

type envelope struct {
	Seq     int             `json:"seq"`
	Status  int             `json:"status"`
	Content json.RawMessage `json:"content"`
}

type loginContent struct {
	SessionID string `json:"session_id"`
}

type headline struct {
	GUID    string `json:"guid"` // usually absent from getHeadlines; falls back to link
	Title   string `json:"title"`
	Link    string `json:"link"`
	Content string `json:"content"` // only present with show_content=true
	Author  string `json:"author"`
	Updated int64  `json:"updated"` // unix seconds
}

// flexInt decodes an id that TT-RSS may send as either a JSON number or a
// quoted string — getCategories returns string ids on some versions while
// getFeeds returns a numeric cat_id, so both ends of the folder mapping must
// tolerate either form.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("ttrss: bad integer id %q: %w", s, err)
	}
	*f = flexInt(n)
	return nil
}

type ttCategory struct {
	ID    flexInt `json:"id"`
	Title string  `json:"title"`
}

type ttFeed struct {
	ID      flexInt `json:"id"`
	Title   string  `json:"title"`
	FeedURL string  `json:"feed_url"`
	CatID   flexInt `json:"cat_id"`
}

func (s *Service) getCategories(ctx context.Context, client *http.Client, endpoint, sid string) ([]ttCategory, error) {
	var cats []ttCategory
	err := s.call(ctx, client, endpoint, map[string]any{
		"op": "getCategories", "sid": sid,
	}, &cats)
	if err != nil {
		return nil, err
	}
	return cats, nil
}

func (s *Service) getFeeds(ctx context.Context, client *http.Client, endpoint, sid string, offset int) ([]ttFeed, error) {
	var fs []ttFeed
	err := s.call(ctx, client, endpoint, map[string]any{
		"op":     "getFeeds",
		"sid":    sid,
		"cat_id": feedsCatAll,
		"limit":  feedsLimit,
		"offset": offset,
	}, &fs)
	if err != nil {
		return nil, err
	}
	return fs, nil
}

func (s *Service) login(ctx context.Context, client *http.Client, endpoint, user, pass string) (string, error) {
	var c loginContent
	err := s.call(ctx, client, endpoint, map[string]any{
		"op": "login", "user": user, "password": pass,
	}, &c)
	if err != nil {
		return "", err
	}
	if c.SessionID == "" {
		return "", errors.New("ttrss: login returned no session id")
	}
	return c.SessionID, nil
}

func (s *Service) logout(ctx context.Context, client *http.Client, endpoint, sid string) {
	_ = s.call(ctx, client, endpoint, map[string]any{"op": "logout", "sid": sid}, nil)
}

func (s *Service) getHeadlines(ctx context.Context, client *http.Client, endpoint, sid string, feedID, skip int) ([]headline, error) {
	var hs []headline
	err := s.call(ctx, client, endpoint, map[string]any{
		"op":           "getHeadlines",
		"sid":          sid,
		"feed_id":      strconv.Itoa(feedID), // string for older-version compat (feed 0)
		"show_content": true,                 // REQUIRED — otherwise content is omitted
		"view_mode":    "all_articles",
		"limit":        headlineLimit,
		"skip":         skip,
	}, &hs)
	if err != nil {
		return nil, err
	}
	return hs, nil
}

// call POSTs a JSON-RPC request to the TT-RSS API endpoint, unwraps the
// {seq,status,content} envelope, and decodes content into out (when non-nil).
func (s *Service) call(ctx context.Context, client *http.Client, endpoint string, payload map[string]any, out any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ttrss: api request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ttrss: API endpoint %s returned HTTP %d — check the URL "+
			"(TT-RSS often lives under a subpath like /tt-rss; enter that full path, "+
			"we append /api/) and that API access is enabled in TT-RSS Preferences",
			endpoint, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("ttrss: decode envelope: %w", err)
	}
	if env.Status != 0 {
		var ae struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(env.Content, &ae)
		msg := ae.Error
		if msg == "" {
			msg = "unknown error"
		}
		// NOT_LOGGED_IN / API_DISABLED surface here verbatim so the user can act.
		return fmt.Errorf("ttrss: api error: %s", msg)
	}
	if out != nil {
		if err := json.Unmarshal(env.Content, out); err != nil {
			return fmt.Errorf("ttrss: decode content: %w", err)
		}
	}
	return nil
}
