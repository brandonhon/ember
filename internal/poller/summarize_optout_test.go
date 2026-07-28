package poller

import (
	"context"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// subscribeTo gives a fresh user a subscription to the feed, opted in or out.
func subscribeTo(t *testing.T, st *store.Store, username string, feedID int64, summarize bool) {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, models.User{Username: username, PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := st.Subscribe(ctx, models.Subscription{UserID: u.ID, FeedID: feedID})
	if err != nil {
		t.Fatal(err)
	}
	if !summarize {
		off := false
		if err := st.UpdateSubscription(ctx, u.ID, sub.ID,
			store.UpdateSubscriptionPatch{Summarize: &off}); err != nil {
			t.Fatal(err)
		}
	}
}

func seedArticle(t *testing.T, st *store.Store, feedID int64, guid string) int64 {
	t.Helper()
	a, _, err := st.UpsertArticle(context.Background(), models.Article{
		FeedID: feedID, GUID: guid, Title: "A Story",
		ContentText: "Body text long enough to summarize.", ContentHash: "h-" + guid,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// The opt-out is enforced at the queue CONSUMER, so it holds no matter which
// producer put the article on the queue.
func TestSummarizeOne_SkipsOptedOutFeed(t *testing.T) {
	p := mkPoller(t, &fakeFetcher{})
	ctx := context.Background()
	f := seedFeed(t, p.Store)
	subscribeTo(t, p.Store, "alice", f.ID, false)
	id := seedArticle(t, p.Store, f.ID, "opted-out")

	p.summarizeOne(ctx, id)

	got, err := p.Store.GetArticle(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "excluded" {
		t.Errorf("summary_model = %q, want %q", got.SummaryModel, "excluded")
	}
	if got.Summary != "" {
		t.Errorf("opted-out feed was summarized anyway: %q", got.Summary)
	}
	// 'excluded' is non-empty on purpose: ListUnsummarizedIDs selects on an
	// EMPTY summary_model, so leaving it NULL would re-queue the article on
	// every tick forever.
	ids, err := p.Store.ListUnsummarizedIDs(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range ids {
		if got == id {
			t.Error("excluded article is still pending — it will be re-queued every tick")
		}
	}
}

// Suppression is unanimous. The summary lives on the shared article row, so one
// subscriber opting out must not take it away from the others.
func TestSummarizeOne_SummarizesWhenAnySubscriberWantsIt(t *testing.T) {
	p := mkPoller(t, &fakeFetcher{})
	ctx := context.Background()
	f := seedFeed(t, p.Store)
	subscribeTo(t, p.Store, "alice", f.ID, false)
	subscribeTo(t, p.Store, "bob", f.ID, true)
	id := seedArticle(t, p.Store, f.ID, "mixed")

	p.summarizeOne(ctx, id)

	got, err := p.Store.GetArticle(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel == "excluded" || got.Summary == "" {
		t.Errorf("bob wants summaries but the article was excluded: model=%q summary=%q",
			got.SummaryModel, got.Summary)
	}
}
