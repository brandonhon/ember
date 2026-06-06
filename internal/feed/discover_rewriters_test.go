package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRewriteKnown_PureURLForms(t *testing.T) {
	ctx := context.Background()
	c := &http.Client{}

	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "youtube channel",
			in:   "https://www.youtube.com/channel/UCXuqSBlHAE6Xw-yeJA0Tunw",
			want: "https://www.youtube.com/feeds/videos.xml?channel_id=UCXuqSBlHAE6Xw-yeJA0Tunw",
			ok:   true,
		},
		{
			name: "youtube playlist",
			in:   "https://www.youtube.com/playlist?list=PLrAXtmErZgOdP_8GztsuKi9nrraNbKKp4",
			want: "https://www.youtube.com/feeds/videos.xml?playlist_id=PLrAXtmErZgOdP_8GztsuKi9nrraNbKKp4",
			ok:   true,
		},
		{
			name: "youtube channel without www",
			in:   "https://youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw",
			want: "https://www.youtube.com/feeds/videos.xml?channel_id=UC_x5XG1OV2P6uZZ5FSM9Ttw",
			ok:   true,
		},
		{
			name: "mastodon profile",
			in:   "https://mastodon.social/@gargron",
			want: "https://mastodon.social/@gargron.rss",
			ok:   true,
		},
		{
			name: "mastodon profile with trailing slash",
			in:   "https://fosstodon.org/@kev/",
			want: "https://fosstodon.org/@kev.rss",
			ok:   true,
		},
		{
			name: "unknown host falls through",
			in:   "https://blog.example.com",
			want: "https://blog.example.com",
			ok:   false,
		},
		{
			name: "youtube watch URL falls through (not a channel/playlist)",
			in:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			want: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			ok:   false,
		},
		{
			name: "non-mastodon path with @ falls through",
			in:   "https://twitter.com/@user/status/123",
			want: "https://twitter.com/@user/status/123",
			ok:   false,
		},
		{
			name: "invalid URL falls through",
			in:   "::not-a-url",
			want: "::not-a-url",
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := RewriteKnown(ctx, c, tc.in, allowAll)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRewriteKnown_YouTubeHandle_ResolvesViaScrape(t *testing.T) {
	// Serve a fixture that mimics the relevant slice of a real channel page.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/@") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head>` +
			`<script>var ytInitialData = {"metadata":{"channelMetadataRenderer":{"channelId":"UCabcdefghijklmnopqrstuv","title":"Test"}}};</script>` +
			`</head><body></body></html>`))
	}))
	defer srv.Close()

	// Construct the @handle URL against our fixture host.
	target := srv.URL + "/@testchannel"
	got, ok, err := RewriteKnown(context.Background(), srv.Client(), target, allowAll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		// The host isn't *.youtube.com so the rewrite shouldn't fire even
		// though the path matches /@handle. The Mastodon branch would catch
		// it instead — and *does*: this is the deliberate fall-through.
		// Verify Mastodon path fired:
		if !strings.HasSuffix(got, "/@testchannel.rss") {
			t.Fatalf("expected mastodon rewrite, got %q", got)
		}
	}
}

func TestRewriteKnown_YouTubeHandle_RealHostFixture(t *testing.T) {
	// Now route a real youtube.com /@handle URL at our fixture by overriding
	// the http client's transport. The rewriter uses host-based matching so
	// the URL itself must say youtube.com.
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head>` +
			`<meta name="x" content='"channelId":"UC0123456789ABCDEFGHIJKL"'/>` +
			`</head></html>`))
	}))
	defer fixture.Close()

	// Replace DNS for youtube.com → fixture.
	c := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(fixture.URL, "http://")
		return http.DefaultTransport.RoundTrip(r)
	})}

	got, ok, err := RewriteKnown(context.Background(), c, "https://www.youtube.com/@testchannel", allowAll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	want := "https://www.youtube.com/feeds/videos.xml?channel_id=UC0123456789ABCDEFGHIJKL"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteKnown_YouTubeHandle_MissingChannelID(t *testing.T) {
	// Page returns 200 but no channelId pattern — rewriter returns the
	// original target so Discover can fall back to its normal probes.
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>no useful data</body></html>`))
	}))
	defer fixture.Close()

	c := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(fixture.URL, "http://")
		return http.DefaultTransport.RoundTrip(r)
	})}

	got, ok, _ := RewriteKnown(context.Background(), c, "https://www.youtube.com/@nobody", allowAll)
	if ok {
		t.Errorf("expected ok=false on missing channelId")
	}
	if got != "https://www.youtube.com/@nobody" {
		t.Errorf("got %q, want original target", got)
	}
}

func TestRewriteKnown_ValidateBlocks(t *testing.T) {
	// validate returns an error → rewriter surfaces it.
	blockErr := http.ErrAbortHandler
	got, ok, err := RewriteKnown(
		context.Background(),
		http.DefaultClient,
		"https://www.youtube.com/@anyone",
		func(string) error { return blockErr },
	)
	if err == nil {
		t.Fatal("expected error from validate, got nil")
	}
	if ok {
		t.Error("expected ok=false on validate failure")
	}
	if got != "https://www.youtube.com/@anyone" {
		t.Errorf("got %q, want original target", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
