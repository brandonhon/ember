package feed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// RewriteKnown handles URL shapes for sites that expose feeds at predictable
// paths but don't advertise them via <link rel="alternate"> on their public
// pages. Saves the user from hand-crafting a feed URL.
//
// Recognized:
//   - https://www.youtube.com/channel/UCxxxxxxxxxxxxxxxxxx
//       → https://www.youtube.com/feeds/videos.xml?channel_id=UCxxxxxxxxxxxxxxxxxx
//   - https://www.youtube.com/playlist?list=PLxxxxxxxxxxxxx
//       → https://www.youtube.com/feeds/videos.xml?playlist_id=PLxxxxxxxxxxxxx
//   - https://www.youtube.com/@handle (or /@handle/videos, /@handle/featured)
//       → resolves the channel UC ID via a one-shot scrape, then maps as above
//   - https://<instance>/@<user>           (Mastodon-compatible)
//       → https://<instance>/@<user>.rss
//
// Returns (rewritten, true, nil) on a successful match. Returns (target,
// false, nil) when no rule applies — the caller should fall back to the
// normal Discover flow. Returns a non-nil error only when a rule recognized
// the shape but the network step failed (e.g. YouTube handle page hit a
// 5xx). The validate function is the same SSRF guard Discover takes; every
// network call routes through it.
func RewriteKnown(ctx context.Context, c *http.Client, target string, validate func(rawURL string) error) (string, bool, error) {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || u.Host == "" {
		return target, false, nil
	}
	host := strings.ToLower(u.Host)

	switch {
	case isYouTubeHost(host):
		// /channel/UC...
		if m := ytChannelPath.FindStringSubmatch(u.Path); m != nil {
			return ytFeedURL("channel_id", m[1]), true, nil
		}
		// /playlist?list=PL...
		if strings.HasPrefix(u.Path, "/playlist") {
			if list := u.Query().Get("list"); ytListID.MatchString(list) {
				return ytFeedURL("playlist_id", list), true, nil
			}
		}
		// /@handle (the modern URL form). Drop trailing /videos, /featured, etc.
		if h := ytHandle.FindStringSubmatch(u.Path); h != nil {
			channelID, err := resolveYouTubeHandle(ctx, c, u, validate)
			if err != nil {
				return target, false, err
			}
			if channelID != "" {
				return ytFeedURL("channel_id", channelID), true, nil
			}
		}
		return target, false, nil

	default:
		// Mastodon-style profile: /@user with no further path segments.
		// Works against Mastodon, Pleroma, Akkoma — all expose <profile>.rss.
		// We deliberately don't poke the network here; if the instance
		// isn't Mastodon-compatible the downstream fetch will fail and
		// Discover's normal fallback path picks back up.
		if mastodonProfile.MatchString(u.Path) {
			alt := *u
			alt.Path = strings.TrimSuffix(alt.Path, "/") + ".rss"
			alt.RawQuery = ""
			alt.Fragment = ""
			return alt.String(), true, nil
		}
	}

	return target, false, nil
}

var (
	// /channel/UC<22 chars>; YouTube channel IDs are always 24 chars starting "UC".
	ytChannelPath = regexp.MustCompile(`^/channel/(UC[0-9A-Za-z_-]{22})/?$`)
	// /playlist?list=PL... ; YouTube playlist IDs start PL/UU/LL/FL/RD and are 13-34 chars.
	ytListID = regexp.MustCompile(`^[A-Za-z0-9_-]{13,34}$`)
	// /@handle, /@handle/videos, /@handle/featured, /@handle/streams, etc.
	ytHandle = regexp.MustCompile(`^/@([A-Za-z0-9._-]{1,30})(?:/[A-Za-z0-9._-]+)?/?$`)
	// Mastodon-style profile: exactly /@username at the root with nothing after.
	// Excludes YouTube's /@handle (which is filtered above by host match).
	mastodonProfile = regexp.MustCompile(`^/@[A-Za-z0-9._-]{1,30}/?$`)
	// "channelId":"UC..." pattern on a YouTube channel HTML page. The handle
	// page is server-rendered enough that this pattern appears in the initial
	// document, no JS execution required.
	ytChannelIDInHTML = regexp.MustCompile(`"channelId":"(UC[0-9A-Za-z_-]{22})"`)
)

func isYouTubeHost(host string) bool {
	return host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com"
}

func ytFeedURL(key, id string) string {
	return "https://www.youtube.com/feeds/videos.xml?" + key + "=" + url.QueryEscape(id)
}

// resolveYouTubeHandle fetches a /@handle page and pulls the UC channel ID
// out of the embedded ytInitialData blob. Returns "" when the pattern isn't
// found (page changed shape, redirect to login, etc.) — the caller treats
// that as "no rewrite, fall back to Discover".
func resolveYouTubeHandle(ctx context.Context, c *http.Client, u *url.URL, validate func(string) error) (string, error) {
	if c == nil {
		c = http.DefaultClient
	}
	page := u.String()
	if err := validate(page); err != nil {
		return "", fmt.Errorf("validate youtube handle: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil
	}
	// 2 MiB cap — the page is typically ~1 MiB; cap is well above and
	// matches the existing fetch.go body cap.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if m := ytChannelIDInHTML.FindSubmatch(body); m != nil {
		return string(m[1]), nil
	}
	return "", nil
}
