package store

import (
	"context"
	"testing"
)

// The global summaries switch has to be a stored value, not a boot-time one:
// issue #198's reporter turned summaries off and a container restart brought
// them straight back, because the only switch was EMBER_DISABLE_SUMMARIES.
func TestSummariesEnabled_PersistsAndOverridesEnv(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()

	// No row: the env-derived fallback decides.
	if !s.ResolveSummariesEnabled(ctx, true) {
		t.Fatal("fallback true should resolve on")
	}
	if s.ResolveSummariesEnabled(ctx, false) {
		t.Fatal("fallback false should resolve off")
	}

	// An explicit off survives even when the env says on — this is the
	// "survives a container restart" requirement from issue #198.
	if err := s.PutSummariesEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if s.ResolveSummariesEnabled(ctx, true) {
		t.Fatal("stored off must beat an env fallback of on")
	}

	// ...and symmetrically, an explicit on beats an env fallback of off, so an
	// admin can enable summaries without editing the environment.
	if err := s.PutSummariesEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if !s.ResolveSummariesEnabled(ctx, false) {
		t.Fatal("stored on must beat an env fallback of off")
	}
}

func TestResetDisabledSummaries_RequeuesOnlyDisabledRows(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, feedID := seedUserAndFeed(t, s, "alice")

	disabled, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "a1", "disabled", "h1", 1000))
	skipped, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "a2", "skipped", "h2", 1000))
	excluded, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "a3", "excluded", "h3", 1000))
	done, _, _ := s.UpsertArticle(ctx, mkArticle(feedID, "a4", "done", "h4", 1000))
	for id, model := range map[int64]string{
		disabled.ID: "disabled", skipped.ID: "skipped",
		excluded.ID: "excluded", done.ID: "qwen2.5:0.5b",
	} {
		if err := s.UpdateSummary(ctx, id, "", model); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := s.ResetDisabledSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != disabled.ID {
		t.Fatalf("reset %v, want only the 'disabled' article %d", ids, disabled.ID)
	}
	// 'skipped' is a genuine failure, 'excluded' is a deliberate opt-out, and a
	// real model name is a finished summary. Turning summaries back on must not
	// burn inference re-doing any of them.
	for _, id := range []int64{skipped.ID, excluded.ID, done.ID} {
		got, err := s.GetArticle(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.SummaryModel == "" {
			t.Fatalf("article %d was reset but should have been left alone", id)
		}
	}
	// The reset row is back in the pending pool, which is what makes the
	// re-enqueue on the API side meaningful.
	got, err := s.GetArticle(ctx, disabled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SummaryModel != "" {
		t.Fatalf("disabled marker survived the reset: summary_model = %q", got.SummaryModel)
	}

	// Nothing left to reset: a second call is a no-op, not an error.
	if ids, err := s.ResetDisabledSummaries(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("second reset = %v, %v; want no ids and no error", ids, err)
	}
}
