package digest

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

func arts(titles ...string) []models.ArticleView {
	out := make([]models.ArticleView, 0, len(titles))
	for i, t := range titles {
		out = append(out, models.ArticleView{
			Article: models.Article{
				ID:      int64(i + 1),
				Title:   t,
				URL:     "https://example.test/" + t,
				Summary: "Lead paragraph for " + t + ".",
			},
		})
	}
	return out
}

// Article titles and summaries come from remote feeds, so the HTML part must
// escape them. An unescaped title would let a hostile feed inject markup into
// the reader's mail client.
func TestRenderHTML_EscapesFeedContent(t *testing.T) {
	a := arts(`<script>alert(1)</script>`)
	a[0].Summary = `evil "quoted" & <b>bold</b>`
	a[0].URL = `https://x.test/?a=1&b=<2>`
	got := renderHTML("Ember", "https://site.test", a)

	if strings.Contains(got, "<script>alert(1)</script>") {
		t.Error("raw <script> from a feed title reached the HTML body")
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("title not HTML-escaped: %s", got)
	}
	if strings.Contains(got, `?a=1&b=<2>`) {
		t.Error("URL not escaped in href")
	}
	if !strings.Contains(got, "&amp;") {
		t.Error("summary/URL ampersand not escaped")
	}
}

func TestRenderHTML_AndText_Structure(t *testing.T) {
	a := arts("First", "Second")
	h := renderHTML("Ember", "https://site.test", a)
	for _, want := range []string{"Ember digest", "2 articles waiting", "First", "Second", "View these on the web"} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	txt := renderText("Ember", "https://site.test", a)
	for _, want := range []string{"Ember digest — 2 articles waiting", "• First", "• Second", "https://site.test"} {
		if !strings.Contains(txt, want) {
			t.Errorf("text missing %q:\n%s", want, txt)
		}
	}
	// Singular has no trailing "s" and no site link when SiteURL is empty.
	one := renderText("Ember", "", arts("Only"))
	if !strings.Contains(one, "1 article waiting") {
		t.Errorf("singular wording wrong: %s", one)
	}
	if strings.Contains(one, "View these on the web") {
		t.Error("site link rendered with no SiteURL")
	}
}

// A missing article URL falls back to the site link; with neither, the title
// renders unlinked rather than emitting href="".
func TestRenderHTML_LinkFallback(t *testing.T) {
	a := arts("T")
	a[0].URL = ""
	withSite := renderHTML("Ember", "https://site.test", a)
	if !strings.Contains(withSite, `href="https://site.test"`) {
		t.Errorf("expected fallback to site URL: %s", withSite)
	}
	noSite := renderHTML("Ember", "", a)
	if strings.Contains(noSite, "href=") {
		t.Errorf("expected no anchor at all: %s", noSite)
	}
}

func TestSummaryParagraph(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"Lead paragraph.", "Lead paragraph."},
		{"\n\n  Lead after blanks.  ", "Lead after blanks."},
		{"• bullet only", ""},
		{"- dash bullet", ""},
		{"Lead.\n• bullet", "Lead."},
		{"\n• bullet first", ""},
	} {
		if got := summaryParagraph(tc.in); got != tc.want {
			t.Errorf("summaryParagraph(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPlural(t *testing.T) {
	if plural(1) != "" {
		t.Error("plural(1) should be empty")
	}
	for _, n := range []int{0, 2, 17} {
		if plural(n) != "s" {
			t.Errorf("plural(%d) should be \"s\"", n)
		}
	}
}

func TestSMTPConfig_Configured(t *testing.T) {
	full := SMTPConfig{Host: "mail.test", Port: 587, From: "a@b.test"}
	if !full.Configured() {
		t.Error("complete config reported unconfigured")
	}
	for _, missing := range []SMTPConfig{
		{Port: 587, From: "a@b.test"},
		{Host: "mail.test", From: "a@b.test"},
		{Host: "mail.test", Port: 587},
		{},
	} {
		if missing.Configured() {
			t.Errorf("incomplete config reported configured: %+v", missing)
		}
	}
}

func TestSMTPAuth_NilWithoutUsername(t *testing.T) {
	s := &Sender{SMTP: SMTPConfig{Host: "h", Port: 1}}
	if s.smtpAuth() != nil {
		t.Error("auth should be nil with no username")
	}
	s.SMTP.Username = "u"
	if s.smtpAuth() == nil {
		t.Error("auth should be set once a username is configured")
	}
}

// SendForUser refuses before doing any work when it has nothing to send to or
// no way to send. These are the guards that keep a misconfigured instance from
// dialing an SMTP relay on every tick.
func TestSendForUser_Guards(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()

	unconfigured := &Sender{Store: st}
	if _, err := unconfigured.SendForUser(ctx, models.User{ID: 1}, models.UserDigest{UserID: 1}); err == nil {
		t.Error("expected an error when SMTP is unconfigured")
	}

	s := &Sender{Store: st, SMTP: SMTPConfig{Host: "mail.test", Port: 587, From: "d@b.test"}}
	if _, err := s.SendForUser(ctx, models.User{ID: 1}, models.UserDigest{UserID: 1}); err == nil {
		t.Error("expected an error when the user has no email and no override")
	}
	// A malformed override is rejected before any send is attempted.
	_, err := s.SendForUser(ctx, models.User{ID: 1, Email: "ok@b.test"},
		models.UserDigest{UserID: 1, EmailOverride: "bad\r\nBcc: evil@x.test"})
	if err == nil {
		t.Error("expected CRLF in the override to be rejected")
	}
}

// No articles => no email, and no error. The runner relies on the 0/nil result
// to skip users with nothing new rather than treating it as a failure.
func TestSendForUser_NoArticlesSendsNothing(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, models.User{Username: "alice", Email: "alice@b.test", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// Host is deliberately unroutable: if this ever tried to send, the test
	// would fail with a dial error instead of passing.
	s := &Sender{Store: st, SMTP: SMTPConfig{Host: "unroutable.invalid", Port: 587, From: "d@b.test", StartTLS: true}}
	n, err := s.SendForUser(ctx, u, models.UserDigest{UserID: u.ID, ViewKind: "smart", ViewValue: "fresh"})
	if err != nil {
		t.Fatalf("SendForUser with no articles: %v", err)
	}
	if n != 0 {
		t.Errorf("sent %d articles, want 0", n)
	}
}

// fetchArticles maps each view kind onto the right store query and applies the
// since-last-send filter.
func TestFetchArticles_ViewKindsAndSinceFilter(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := st.UpsertFeed(ctx, models.Feed{URL: "https://f.test/rss", Title: "F"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for i := range 3 {
		if _, _, err := st.UpsertArticle(ctx, models.Article{
			FeedID: f.ID, GUID: string(rune('a' + i)), Title: "T", ContentHash: string(rune('h' + i)),
			PublishedAt: now - int64(i*60), SummaryModel: "noop", Summary: "Lead.",
		}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Sender{Store: st}

	got, err := s.fetchArticles(ctx, models.UserDigest{UserID: u.ID, ViewKind: "feed", ViewValue: "999999"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("unknown feed id returned %d articles, want 0", len(got))
	}

	got, err = s.fetchArticles(ctx, models.UserDigest{UserID: u.ID, ViewKind: "smart", ViewValue: "today"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("smart/today returned %d, want 3", len(got))
	}

	// LastSentAt in the future filters everything out.
	got, err = s.fetchArticles(ctx, models.UserDigest{
		UserID: u.ID, ViewKind: "smart", ViewValue: "today", LastSentAt: now + 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("since-filter returned %d, want 0", len(got))
	}
}

// The MIME payload must actually parse as multipart/alternative with a text
// and an HTML part — string-matching the assembly would not catch a malformed
// boundary or a missing terminator.
func TestBuildMIME_ParsesAsMultipart(t *testing.T) {
	raw, err := buildMIME("from@b.test", "to@b.test", "Subj", "plain body", "<p>html body</p>")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("payload is not a parseable message: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("media type = %q, want multipart/alternative", mediaType)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var got []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		body, _ := io.ReadAll(p)
		got = append(got, p.Header.Get("Content-Type")+"|"+strings.TrimSpace(string(body)))
	}
	if len(got) != 2 {
		t.Fatalf("got %d parts, want 2: %v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "text/plain") || !strings.HasSuffix(got[0], "plain body") {
		t.Errorf("part 0 = %q", got[0])
	}
	if !strings.HasPrefix(got[1], "text/html") || !strings.HasSuffix(got[1], "<p>html body</p>") {
		t.Errorf("part 1 = %q", got[1])
	}
}

// Article titles reach the text/plain part verbatim, so the boundary is the
// only thing stopping a hostile feed from closing the part early and forging
// MIME sections. It must therefore be unpredictable per message — which is why
// buildMIME treats a crypto/rand failure as a hard error rather than falling
// back to a fixed boundary.
func TestBuildMIME_BoundaryIsUnpredictable(t *testing.T) {
	seen := make(map[string]bool, 16)
	for range 16 {
		raw, err := buildMIME("f@b.test", "t@b.test", "S", "x", "<p>x</p>")
		if err != nil {
			t.Fatal(err)
		}
		_, params, err := mime.ParseMediaType(mustHeader(t, raw, "Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		b := params["boundary"]
		if b == "ember-0000000000000000" {
			t.Fatal("boundary is the all-zero fallback — rand failure was swallowed")
		}
		if seen[b] {
			t.Fatalf("boundary repeated across messages: %q", b)
		}
		seen[b] = true
	}
}

// A feed article whose title contains a boundary-shaped line must not be able
// to terminate the real part: the actual boundary is unguessable, so the
// injected text stays inside the body where it belongs.
func TestRenderText_HostileTitleCannotForgeMIMEPart(t *testing.T) {
	hostile := arts("Legit\r\n--ember-0000000000000000\r\nContent-Type: text/html\r\n\r\n<h1>forged</h1>")
	raw, err := buildMIME("f@b.test", "t@b.test", "S",
		renderText("Ember", "", hostile), renderHTML("Ember", "", hostile))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	n := 0
	for {
		if _, err := mr.NextPart(); err != nil {
			break
		}
		n++
	}
	if n != 2 {
		t.Errorf("hostile title produced %d MIME parts, want exactly 2", n)
	}
}

func mustHeader(t *testing.T, raw []byte, key string) string {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return msg.Header.Get(key)
}
