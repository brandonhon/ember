package opml

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

func seedUser(t *testing.T, st *store.Store, name string) int64 {
	t.Helper()
	u, err := st.CreateUser(context.Background(), models.User{Username: name, PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// Export must place categorized feeds inside their folder outline and
// uncategorized ones at the top level, and the result must re-import to the
// same shape (the round trip is the actual contract users rely on).
func TestExport_StructureAndRoundTrip(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	uid := seedUser(t, st, "alice")
	svc := NewService(st)

	cat, err := st.CreateCategory(ctx, models.Category{UserID: uid, Name: "Tech"})
	if err != nil {
		t.Fatal(err)
	}
	inCat, err := st.UpsertFeed(ctx, models.Feed{URL: "https://a.test/rss", Title: "A", SiteURL: "https://a.test"})
	if err != nil {
		t.Fatal(err)
	}
	loose, err := st.UpsertFeed(ctx, models.Feed{URL: "https://b.test/rss", Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: uid, FeedID: inCat.ID, CategoryID: &cat.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: uid, FeedID: loose.ID}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := svc.Export(ctx, uid, &buf); err != nil {
		t.Fatalf("Export: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, xml.Header) {
		t.Error("export missing XML header")
	}

	var doc opmlDoc
	if err := xml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not valid OPML: %v", err)
	}
	if doc.Version != "2.0" || doc.Head.Title != "ember subscriptions" {
		t.Errorf("head = %+v", doc.Head)
	}
	var folder, top *outline
	for i := range doc.Body.Outlines {
		o := &doc.Body.Outlines[i]
		switch {
		case o.Title == "Tech":
			folder = o
		case o.XMLURL == "https://b.test/rss":
			top = o
		}
	}
	if folder == nil || len(folder.Outlines) != 1 || folder.Outlines[0].XMLURL != "https://a.test/rss" {
		t.Fatalf("categorized feed not nested under its folder: %+v", doc.Body.Outlines)
	}
	if folder.Outlines[0].HTMLURL != "https://a.test" {
		t.Errorf("htmlUrl not exported: %+v", folder.Outlines[0])
	}
	if top == nil {
		t.Fatal("uncategorized feed missing from the top level")
	}

	// Round trip into a second user: same folder + feeds land.
	other := seedUser(t, st, "bob")
	n, err := svc.Import(ctx, other, strings.NewReader(out))
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if n != 2 {
		t.Errorf("round-trip import = %d, want 2", n)
	}
	cats, err := st.ListCategories(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 1 || cats[0].Name != "Tech" {
		t.Errorf("round-trip categories = %+v, want [Tech]", cats)
	}
}

func TestExport_EmptyAccountIsValidOPML(t *testing.T) {
	st := store.NewTest(t)
	uid := seedUser(t, st, "alice")
	var buf bytes.Buffer
	if err := NewService(st).Export(context.Background(), uid, &buf); err != nil {
		t.Fatal(err)
	}
	var doc opmlDoc
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("empty export is not valid OPML: %v", err)
	}
	if len(doc.Body.Outlines) != 0 {
		t.Errorf("empty account exported %d outlines", len(doc.Body.Outlines))
	}
}

func TestWriteExport_CreatesTimestampedFile(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	uid := seedUser(t, st, "alice")
	f, err := st.UpsertFeed(ctx, models.Feed{URL: "https://a.test/rss", Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: uid, FeedID: f.ID}); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "nested", "exports") // must be created for us
	path, size, err := NewService(st).WriteExport(ctx, uid, dir)
	if err != nil {
		t.Fatalf("WriteExport: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("wrote to %s, want a file inside %s", path, dir)
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "ember-") || !strings.HasSuffix(base, ".opml") {
		t.Errorf("filename = %q, want ember-<timestamp>.opml", base)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != size || size == 0 {
		t.Errorf("reported size %d, on-disk %d", size, fi.Size())
	}
	// The write-probe file must not be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ember-export-writetest") {
			t.Errorf("write-probe file left behind: %s", e.Name())
		}
	}
}

func TestWriteExport_Rejects(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	uid := seedUser(t, st, "alice")
	svc := NewService(st)

	if _, _, err := svc.WriteExport(ctx, uid, ""); err == nil {
		t.Error("empty directory accepted")
	}

	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory write permission bits")
	}
	// An existing but unwritable directory must fail with the actionable
	// message rather than a bare filesystem error later on.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, _, err := svc.WriteExport(ctx, uid, dir)
	if err == nil {
		t.Fatal("unwritable directory accepted")
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Errorf("error = %v, want it to say the directory is not writable", err)
	}
}

// A validator that rejects a URL must skip that feed and keep importing the
// rest — an SSRF-blocked entry can't abort the whole file.
func TestImport_ValidatorSkipsRejectedFeeds(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	uid := seedUser(t, st, "alice")
	svc := NewService(st)
	svc.ValidateURL = func(_ context.Context, raw string) error {
		if strings.Contains(raw, "blocked") {
			return errors.New("ssrf")
		}
		return nil
	}
	doc := `<?xml version="1.0"?><opml version="2.0"><body>
	  <outline type="rss" title="ok" xmlUrl="https://ok.test/rss"/>
	  <outline type="rss" title="bad" xmlUrl="http://blocked.internal/rss"/>
	</body></opml>`
	n, err := svc.Import(ctx, uid, strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("import = %d, want 1 (the blocked feed is skipped)", n)
	}
	feeds, err := st.ListFeedsForUser(ctx, uid, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range feeds {
		if strings.Contains(f.URL, "blocked") {
			t.Errorf("blocked URL was subscribed: %s", f.URL)
		}
	}
}

// Malformed XML is an error, not a silent zero-import.
func TestImport_MalformedXML(t *testing.T) {
	st := store.NewTest(t)
	uid := seedUser(t, st, "alice")
	if _, err := NewService(st).Import(context.Background(), uid, strings.NewReader("<opml><body>")); err == nil {
		t.Error("truncated OPML accepted")
	}
}
