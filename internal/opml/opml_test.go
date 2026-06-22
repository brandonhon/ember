package opml

import (
	"context"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// TestImportCategorizes verifies the import categorization contract: feeds in a
// folder join that folder's category, nested sub-folders flatten into the
// top-level folder's category, and top-level feeds stay uncategorized.
func TestImportCategorizes(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}

	const doc = `<?xml version="1.0"?>
<opml version="2.0">
  <body>
    <outline title="News">
      <outline type="rss" title="BBC" xmlUrl="https://bbc.test/rss" htmlUrl="https://bbc.test"/>
      <outline title="World">
        <outline type="rss" title="Reuters" xmlUrl="https://reuters.test/rss"/>
      </outline>
    </outline>
    <outline type="rss" title="Loose" xmlUrl="https://loose.test/rss"/>
  </body>
</opml>`

	svc := NewService(st)
	n, err := svc.Import(ctx, u.ID, strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("created = %d, want 3", n)
	}

	cats, err := st.ListCategories(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	catByName := map[string]int64{}
	for _, c := range cats {
		catByName[c.Name] = c.ID
	}
	// Only top-level folders become categories; the nested "World" folder flattens.
	if _, ok := catByName["News"]; !ok {
		t.Fatalf("no 'News' category; got %v", catByName)
	}
	if _, ok := catByName["World"]; ok {
		t.Fatalf("nested 'World' folder should not create a category; got %v", catByName)
	}

	feeds, err := st.ListFeedsForUser(ctx, u.ID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	gotCat := map[string]*int64{}
	for _, f := range feeds {
		gotCat[f.Title] = f.CategoryID
	}

	newsID := catByName["News"]
	wantNews := func(title string) {
		t.Helper()
		c := gotCat[title]
		if c == nil || *c != newsID {
			t.Fatalf("%s category = %v, want News (%d)", title, c, newsID)
		}
	}
	wantNews("BBC")     // feed directly in the folder
	wantNews("Reuters") // feed in a nested sub-folder, flattened into News

	if c := gotCat["Loose"]; c != nil {
		t.Fatalf("top-level feed 'Loose' category = %v, want nil (uncategorized)", c)
	}
}

// TestImportIsIdempotent verifies a re-import of the same OPML doesn't create
// duplicate subscriptions or categories, and that a feed first imported
// uncategorized picks up a category when a later import places it in a folder.
func TestImportIsIdempotent(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(st)

	// First import: BBC is uncategorized (top level).
	const first = `<?xml version="1.0"?>
<opml version="2.0"><body>
  <outline type="rss" title="BBC" xmlUrl="https://bbc.test/rss"/>
</body></opml>`
	if _, err := svc.Import(ctx, u.ID, strings.NewReader(first)); err != nil {
		t.Fatal(err)
	}

	// Second import: same feed, now inside a "News" folder.
	const second = `<?xml version="1.0"?>
<opml version="2.0"><body>
  <outline title="News">
    <outline type="rss" title="BBC" xmlUrl="https://bbc.test/rss"/>
  </outline>
</body></opml>`
	if _, err := svc.Import(ctx, u.ID, strings.NewReader(second)); err != nil {
		t.Fatal(err)
	}

	// Still exactly one subscription — no duplicate from the re-import.
	feeds, err := st.ListFeedsForUser(ctx, u.ID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 {
		t.Fatalf("subscriptions = %d, want 1 (no duplicate on re-import)", len(feeds))
	}

	// Exactly one "News" category, and BBC now belongs to it (the best-effort
	// upgrade of a previously-uncategorized subscription).
	cats, err := st.ListCategories(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 1 || cats[0].Name != "News" {
		t.Fatalf("categories = %+v, want exactly one 'News'", cats)
	}
	if feeds[0].CategoryID == nil || *feeds[0].CategoryID != cats[0].ID {
		t.Fatalf("BBC category = %v, want News (%d) after re-import", feeds[0].CategoryID, cats[0].ID)
	}
}
