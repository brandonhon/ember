// Package updatecheck polls the GitHub Releases API for the latest tagged
// Ember release and compares it against the running build. The result is
// cached in memory and surfaced to admins via /api/me so the SPA can show an
// "update available" hint. It sends no telemetry — the only outbound call is a
// plain unauthenticated GET of a public endpoint.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

// DefaultInterval is how often the checker re-polls GitHub after the first
// check. One call/day stays far under the unauthenticated 60 req/hr/IP limit.
const DefaultInterval = 24 * time.Hour

// initialDelay staggers the first check a little past boot so it never competes
// with startup work (migrations, first feed refresh).
const initialDelay = 30 * time.Second

// Result is the cached outcome of the most recent successful check.
type Result struct {
	Current   string    `json:"current"`    // this build's version, e.g. "v0.9.4"
	Latest    string    `json:"latest"`     // latest release tag, e.g. "v0.9.5"
	Available bool      `json:"available"`  // Latest is newer than Current per semver
	URL       string    `json:"url"`        // release page (html_url)
	Published time.Time `json:"published"`  // release published_at
	CheckedAt time.Time `json:"checked_at"` // when this result was fetched
}

// Checker holds the current version and the cached latest-release result.
// Latest() is safe to call concurrently with the background Run loop.
type Checker struct {
	current string
	repo    string // "owner/name"
	baseURL string // GitHub API base; overridable in tests
	client  *http.Client
	log     *slog.Logger

	mu         sync.RWMutex
	result     Result
	haveResult bool
}

// githubAPIBase is the default GitHub REST API host.
const githubAPIBase = "https://api.github.com"

// New returns a Checker for the given build version and "owner/name" repo.
// current should be a clean release tag (e.g. "v0.9.4"); see IsReleaseVersion.
func New(current, repo string, log *slog.Logger) *Checker {
	if log == nil {
		log = slog.Default()
	}
	return &Checker{
		current: current,
		repo:    repo,
		baseURL: githubAPIBase,
		client:  &http.Client{Timeout: 10 * time.Second},
		log:     log,
	}
}

// IsReleaseVersion reports whether v is a clean tagged release (valid semver
// with no prerelease/build suffix). Between-tag builds from `git describe`
// (e.g. "v0.9.4-5-gabc123", "v0.9.4-dirty") and bare commit SHAs return false,
// so dev builds never nag about updates and never risk a false "update
// available" from comparing an ahead-of-tag build against the last release.
func IsReleaseVersion(v string) bool {
	return semver.IsValid(v) && semver.Prerelease(v) == "" && semver.Build(v) == ""
}

// Latest returns the cached result. ok is false until the first successful
// check completes.
func (c *Checker) Latest() (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.result, c.haveResult
}

// Run polls GitHub once after a short delay, then every interval, until ctx is
// cancelled. enabled is resolved before each poll so an admin can turn the
// check off at runtime (env EMBER_DISABLE_UPDATE_CHECK sets the default); when
// it returns false the poll is skipped and the cache is cleared.
func (c *Checker) Run(ctx context.Context, interval time.Duration, enabled func() bool) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	timer := time.NewTimer(initialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if enabled == nil || enabled() {
				if err := c.checkOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
					c.log.Warn("update check failed", "err", err)
				}
			} else {
				c.clear()
			}
			timer.Reset(interval)
		}
	}
}

// clear drops any cached result so a disabled check stops advertising an
// update on /api/me.
func (c *Checker) clear() {
	c.mu.Lock()
	c.result, c.haveResult = Result{}, false
	c.mu.Unlock()
}

// ghRelease is the subset of the GitHub Releases API payload we consume. The
// /releases/latest endpoint already excludes drafts and prereleases, so we
// only need the tag, link, and publish time.
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// checkOnce fetches the latest release and updates the cache. It leaves the
// previous cached result intact on error.
func (c *Checker) checkOnce(ctx context.Context) error {
	if !IsReleaseVersion(c.current) {
		// Dev/dirty build — nothing to compare against.
		return nil
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.baseURL, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// GitHub requires a User-Agent and recommends pinning the API version.
	req.Header.Set("User-Agent", "ember-update-check")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return err
	}
	if !semver.IsValid(rel.TagName) {
		return fmt.Errorf("github releases: invalid tag %q", rel.TagName)
	}

	res := Result{
		Current:   c.current,
		Latest:    rel.TagName,
		Available: semver.Compare(rel.TagName, c.current) > 0,
		URL:       rel.HTMLURL,
		Published: rel.PublishedAt,
		CheckedAt: time.Now(),
	}
	c.mu.Lock()
	c.result, c.haveResult = res, true
	c.mu.Unlock()
	return nil
}
