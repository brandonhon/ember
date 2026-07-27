package store

import (
	"context"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// The sidebar contract: every badge equals the length of the list you land on
// when you click it. TestCountArticles_MatchesList covers one view in
// isolation; this drives all five through CountSmartViews — the exact call the
// /api/smart-counts handler makes — against one messy fixture that exercises
// muted feeds, cross-feed duplicates, read state, the unread window, the
// summary gate, and shares at the same time.
func TestCountSmartViews_AllBadgesMatchTheirLists(t *testing.T) {
	for _, onlySummarized := range []bool{false, true} {
		name := "summary gate off"
		if onlySummarized {
			name = "summary gate on"
		}
		t.Run(name, func(t *testing.T) {
			s := NewTest(t)
			now := time.Unix(1_700_000_000, 0)
			s.Now = func() time.Time { return now }
			ctx := context.Background()

			userID, feedA := seedUserAndFeed(t, s, "alice")
			// A second subscribed feed that republishes one of feedA's stories,
			// plus a muted feed whose items must never reach a smart view.
			feedB, err := s.UpsertFeed(ctx, models.Feed{URL: "https://b.test/feed", Title: "B"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Subscribe(ctx, models.Subscription{UserID: userID, FeedID: feedB.ID}); err != nil {
				t.Fatal(err)
			}
			feedM, err := s.UpsertFeed(ctx, models.Feed{URL: "https://m.test/feed", Title: "Muted"})
			if err != nil {
				t.Fatal(err)
			}
			subM, err := s.Subscribe(ctx, models.Subscription{UserID: userID, FeedID: feedM.ID})
			if err != nil {
				t.Fatal(err)
			}
			muted := true
			if err := s.UpdateSubscription(ctx, userID, subM.ID, UpdateSubscriptionPatch{Muted: &muted}); err != nil {
				t.Fatal(err)
			}

			add := func(feedID int64, guid, title string, ageHours int, summarized bool) models.Article {
				t.Helper()
				a := mkArticle(feedID, guid, title, "h-"+guid,
					now.Add(-time.Duration(ageHours)*time.Hour).Unix())
				if summarized {
					a.Summary = "s"
					a.SummaryModel = "test-model"
				}
				got, _, err := s.UpsertArticle(ctx, a)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}

			// Fresh window (6h) and unread window (24h) content.
			recent1 := add(feedA, "recent-1", "Recent One", 1, true)
			_ = add(feedA, "recent-2", "Recent Two", 2, true)
			// Unsummarized: visible only when the gate is off.
			_ = add(feedA, "recent-3", "Recent Three", 3, false)
			// Inside the unread window but outside the fresh window.
			midA := add(feedA, "mid-1", "Mid One", 12, true)
			// Cross-feed duplicate of mid-1 — same title fingerprint, so only the
			// lowest-id copy may be counted or listed.
			_ = add(feedB.ID, "mid-1-dup", "Mid One", 12, true)
			// Outside the unread window entirely.
			_ = add(feedA, "old-1", "Old One", 72, true)
			// Muted feed: must not appear in any smart view.
			_ = add(feedM.ID, "muted-1", "Muted One", 1, true)
			// Already read: drops out of fresh + unread, stays in starred/later.
			readOne := add(feedA, "read-1", "Read One", 2, true)
			if err := s.SetRead(ctx, userID, []int64{readOne.ID}, true); err != nil {
				t.Fatal(err)
			}

			// Star and save-for-later a mix, including a read one and a muted one.
			for _, id := range []int64{recent1.ID, readOne.ID} {
				if err := s.SetStarred(ctx, userID, id, true); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetLater(ctx, userID, midA.ID, true); err != nil {
				t.Fatal(err)
			}

			// A share received from another user.
			bob, err := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
			if err != nil {
				t.Fatal(err)
			}
			// CreateShare requires the sender to be subscribed to the article's feed.
			if _, err := s.Subscribe(ctx, models.Subscription{UserID: bob.ID, FeedID: feedA}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateShare(ctx, models.Share{
				ArticleID: recent1.ID, FromUser: bob.ID, ToUser: userID, Note: "read this",
			}); err != nil {
				t.Fatal(err)
			}

			freshWindow := 6 * time.Hour
			unreadCutoff := now.Add(-24 * time.Hour).Unix()
			counts, err := s.CountSmartViews(ctx, userID, freshWindow, unreadCutoff, onlySummarized)
			if err != nil {
				t.Fatal(err)
			}

			// Each badge is compared against the list the SPA loads for that
			// view, built with the same parameters the article handler derives.
			cases := []struct {
				view  string
				badge int
				query ListArticlesQuery
			}{
				{"fresh", counts.Fresh, ListArticlesQuery{
					View: "fresh", FreshAfter: now.Add(-freshWindow).Unix(), OnlySummarized: onlySummarized,
				}},
				{"unread", counts.Unread, ListArticlesQuery{
					View: "unread", FreshAfter: unreadCutoff, OnlySummarized: onlySummarized,
				}},
				{"starred", counts.Starred, ListArticlesQuery{
					View: "starred", OnlySummarized: onlySummarized,
				}},
				{"later", counts.Later, ListArticlesQuery{
					View: "later", OnlySummarized: onlySummarized,
				}},
			}
			for _, tc := range cases {
				tc.query.Limit = 500 // well above the fixture, so paging can't mask a gap
				list, err := s.ListArticles(ctx, userID, tc.query)
				if err != nil {
					t.Fatalf("%s: %v", tc.view, err)
				}
				if tc.badge != len(list) {
					t.Errorf("%s badge=%d but list has %d articles — badge must equal its column",
						tc.view, tc.badge, len(list))
					for _, a := range list {
						t.Logf("  %s listed: id=%d feed=%d title=%q published=%d",
							tc.view, a.ID, a.FeedID, a.Title, a.PublishedAt)
					}
				}
			}

			// Shared is counted from the shares table (unseen), while the list is
			// every share received. They are deliberately different questions, so
			// assert the badge against unseen shares rather than list length.
			sharedList, err := s.ListArticles(ctx, userID, ListArticlesQuery{View: "shared", Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			if counts.Shared > len(sharedList) {
				t.Errorf("shared badge=%d exceeds the %d shares in the list", counts.Shared, len(sharedList))
			}
		})
	}
}

// Muted feeds and cross-feed duplicates are exactly the cases where a naive
// count over article_state drifts from the list, so pin the expected numbers
// too — parity alone would pass if both sides were wrong in the same way.
func TestCountSmartViews_ExcludesMutedAndDuplicates(t *testing.T) {
	s := NewTest(t)
	now := time.Unix(1_700_000_000, 0)
	s.Now = func() time.Time { return now }
	ctx := context.Background()

	userID, feedA := seedUserAndFeed(t, s, "alice")
	feedB, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://b.test/feed", Title: "B"})
	_, _ = s.Subscribe(ctx, models.Subscription{UserID: userID, FeedID: feedB.ID})
	feedM, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://m.test/feed", Title: "Muted"})
	subM, _ := s.Subscribe(ctx, models.Subscription{UserID: userID, FeedID: feedM.ID})
	muted := true
	if err := s.UpdateSubscription(ctx, userID, subM.ID, UpdateSubscriptionPatch{Muted: &muted}); err != nil {
		t.Fatal(err)
	}

	mk := func(feedID int64, guid, title string) {
		a := mkArticle(feedID, guid, title, "h-"+guid, now.Add(-time.Hour).Unix())
		a.Summary, a.SummaryModel = "s", "test-model"
		if _, _, err := s.UpsertArticle(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	mk(feedA, "unique-1", "Unique One")
	mk(feedA, "shared-story", "Shared Story")
	mk(feedB.ID, "shared-story-dup", "Shared Story") // duplicate: must collapse
	mk(feedM.ID, "muted-1", "Muted One")             // muted: must not count

	counts, err := s.CountSmartViews(ctx, userID, 6*time.Hour, now.Add(-24*time.Hour).Unix(), true)
	if err != nil {
		t.Fatal(err)
	}
	// Unique One + one copy of Shared Story = 2.
	if counts.Unread != 2 {
		t.Errorf("unread badge = %d, want 2 (duplicate collapsed, muted excluded)", counts.Unread)
	}
	if counts.Fresh != 2 {
		t.Errorf("fresh badge = %d, want 2 (duplicate collapsed, muted excluded)", counts.Fresh)
	}
}
