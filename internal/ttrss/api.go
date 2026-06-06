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
	"github.com/brandonhon/ember/internal/urlcheck"
)

// TT-RSS virtual feed IDs (see API reference): -1 = Starred, 0 = Archived.
const (
	feedStarred  = -1
	feedArchived = 0
)

const (
	headlineLimit  = 200      // page size for getHeadlines
	maxArticles    = 100_000  // safety cap on a single pull
	maxAPIResponse = 16 << 20 // cap per API response body
	apiCallTimeout = 30 * time.Second
)

// APIOptions configures a live pull from a running TT-RSS instance.
type APIOptions struct {
	BaseURL        string
	Username       string
	Password       string
	ImportStarred  bool
	ImportArchived bool
}

// ImportFromAPI logs into a running TT-RSS instance and pulls the user's
// Starred and/or Archived articles via getHeadlines, storing them in the same
// import feed (starred + read) as the file path. Credentials are used only for
// this call and never persisted.
//
// Note: the TT-RSS JSON API is disabled by default — the source user must
// enable "API access" in their TT-RSS preferences first.
func (s *Service) ImportFromAPI(ctx context.Context, userID int64, opt APIOptions) (Result, error) {
	var res Result
	if !opt.ImportStarred && !opt.ImportArchived {
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
	return res, nil
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
		c.CheckRedirect = feed.RedirectGuard(func(raw string) error {
			return s.ValidateURL(ctx, raw)
		})
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
