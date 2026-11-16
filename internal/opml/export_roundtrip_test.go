package opml

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// Export writes an empty folder as a childless outline; Import must still
// recreate it, or empty folders are lost on every round trip.
func TestImport_EmptyFolderSurvivesRoundTrip(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	uid := seedUser(t, st, "alice")
	svc := NewService(st)
	if _, err := st.CreateCategory(ctx, models.Category{UserID: uid, Name: "Later"}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := svc.Export(ctx, uid, &buf); err != nil {
		t.Fatal(err)
	}
	other := seedUser(t, st, "bob")
	if _, err := svc.Import(ctx, other, strings.NewReader(buf.String())); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	cats, err := st.ListCategories(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 1 || cats[0].Name != "Later" {
		t.Errorf("round-trip categories = %+v, want [Later]\nexport:\n%s", cats, buf.String())
	}
}

// Export must carry the user's renamed title, not the upstream feed title.
func TestExport_UsesTitleOverride(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	uid := seedUser(t, st, "alice")
	renamed, err := st.UpsertFeed(ctx, models.Feed{URL: "https://a.test/rss", Title: "Upstream A"})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := st.UpsertFeed(ctx, models.Feed{URL: "https://b.test/rss", Title: "Upstream B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: uid, FeedID: renamed.ID, TitleOverride: "My A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Subscribe(ctx, models.Subscription{UserID: uid, FeedID: plain.ID}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := NewService(st).Export(ctx, uid, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `title="My A"`) || !strings.Contains(out, `text="My A"`) || strings.Contains(out, "Upstream A") {
		t.Errorf("override not exported:\n%s", out)
	}
	if !strings.Contains(out, `title="Upstream B"`) {
		t.Errorf("feed without override should export its upstream title:\n%s", out)
	}
}
