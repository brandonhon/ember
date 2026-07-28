package opml

import (
	"context"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

const twoFeeds = `<?xml version="1.0"?>
<opml version="2.0"><head><title>t</title></head><body>
  <outline title="Tech" text="Tech">
    <outline type="rss" title="A" xmlUrl="https://a.test/rss" htmlUrl="https://a.test"/>
    <outline type="rss" title="B" xmlUrl="https://b.test/rss"/>
  </outline>
  <outline type="rss" title="C" xmlUrl="https://c.test/rss"/>
</body></opml>`

// Import documents itself as returning "the number of *new* subscriptions
// created". Re-importing the same file must therefore report 0, not repeat the
// original count.
func TestImport_CountsOnlyNewSubscriptions(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(st)

	first, err := svc.Import(ctx, u.ID, strings.NewReader(twoFeeds))
	if err != nil {
		t.Fatal(err)
	}
	if first != 3 {
		t.Fatalf("first import = %d, want 3", first)
	}

	second, err := svc.Import(ctx, u.ID, strings.NewReader(twoFeeds))
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Errorf("re-import of the same OPML = %d, want 0 (nothing new was created)", second)
	}

	// The subscriptions themselves must not have been duplicated.
	feeds, err := st.ListFeedsForUser(ctx, u.ID, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 3 {
		t.Errorf("subscriptions after two imports = %d, want 3", len(feeds))
	}
}

// A file listing the same URL twice must count it once.
func TestImport_DuplicateURLWithinOneFileCountsOnce(t *testing.T) {
	st := store.NewTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	dup := `<?xml version="1.0"?><opml version="2.0"><body>
	  <outline type="rss" title="A" xmlUrl="https://a.test/rss"/>
	  <outline type="rss" title="A again" xmlUrl="https://a.test/rss"/>
	</body></opml>`
	n, err := NewService(st).Import(ctx, u.ID, strings.NewReader(dup))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("import with a duplicated URL = %d, want 1", n)
	}
}
