package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

// A share lands in the recipient's Shared view even when they don't follow
// the article's feed, so opening it, starring it, and marking it read must
// work too — not 404 and silently no-op. Access stays user-scoped: a third
// user with neither a subscription nor a share still gets nothing.
func TestShares_RecipientWithoutSubscriptionCanOpenAndMark(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	carol, _ := s.CreateUser(ctx, models.User{Username: "carol", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: alice.ID, FeedID: f.ID})
	art, _, _ := s.UpsertArticle(ctx, mkArticle(f.ID, "g1", "Shared Story", "h1", 1000))
	if _, err := s.CreateShare(ctx, models.Share{ArticleID: art.ID, FromUser: alice.ID, ToUser: bob.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.GetArticleForUser(ctx, bob.ID, art.ID); err != nil {
		t.Errorf("recipient GetArticleForUser: %v, want the article", err)
	}
	if err := s.SetRead(ctx, bob.ID, []int64{art.ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStarred(ctx, bob.ID, art.ID, true); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.GetState(ctx, bob.ID, art.ID); !st.IsRead || !st.IsStarred {
		t.Errorf("recipient state = %+v, want read+starred", st)
	}

	if _, err := s.GetArticleForUser(ctx, carol.ID, art.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unrelated GetArticleForUser err = %v, want ErrNotFound", err)
	}
	if err := s.SetRead(ctx, carol.ID, []int64{art.ID}, true); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.GetState(ctx, carol.ID, art.ID); st.IsRead {
		t.Error("unrelated user wrote state for an article neither subscribed to nor shared with them")
	}
}

// A tag is an explicit per-article label, so the tag view must list every
// article carrying it — including one whose untagged lower-id duplicate would
// otherwise suppress it via cross-feed dedup. Otherwise ListUserTags reports
// the tag with count 1 while the tag view is empty.
func TestTagView_NotSuppressedByUntaggedDuplicate(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f1, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f1.test/feed", Title: "F1"})
	f2, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://f2.test/feed", Title: "F2"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f1.ID})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f2.ID})

	const dupURL = "https://news.test/big-story"
	_, _, _ = s.UpsertArticle(ctx, models.Article{FeedID: f1.ID, GUID: "g1", Title: "Big Story", URL: dupURL, ContentHash: "h1", PublishedAt: 1000})
	tagged, _, _ := s.UpsertArticle(ctx, models.Article{FeedID: f2.ID, GUID: "g2", Title: "Big Story", URL: dupURL, ContentHash: "h2", PublishedAt: 1001})
	if err := s.AddArticleTag(ctx, u.ID, tagged.ID, "keep"); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListArticles(ctx, u.ID, ListArticlesQuery{Tag: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != tagged.ID {
		t.Errorf("tag view = %v, want the tagged article", titles(list))
	}
}

// A board is explicit curation: muting the feed an article came from must not
// pull it off a board the user pinned it to.
func TestBoardView_KeepsArticleFromMutedFeed(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	sub, _ := s.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: f.ID})
	art, _, _ := s.UpsertArticle(ctx, mkArticle(f.ID, "g1", "Pinned", "h1", 1000))
	b, err := s.CreateBoard(ctx, models.Board{UserID: u.ID, Name: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddArticleToBoard(ctx, u.ID, b.ID, art.ID); err != nil {
		t.Fatal(err)
	}
	muted := true
	if err := s.UpdateSubscription(ctx, u.ID, sub.ID, UpdateSubscriptionPatch{Muted: &muted}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListArticles(ctx, u.ID, ListArticlesQuery{BoardID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != art.ID {
		t.Errorf("board view = %v, want the pinned article despite its feed being muted", titles(list))
	}
}
