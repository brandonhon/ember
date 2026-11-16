package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/digest"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// A failed send used to be marked sent, so the window it should have covered
// was skipped and its articles never reached any digest. It must stay unsent,
// back off instead of retrying every tick, and go out once SMTP recovers.
func TestTickDigest_FailedSendIsRetriedNotMarkedSent(t *testing.T) {
	ctx := context.Background()
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.NewTest(t)

	u, err := st.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x", Email: "alice@example.com"})
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
	now := time.Now().UTC().Truncate(time.Second)
	lastSent := now.Add(-24 * time.Hour)
	fetched := now.Add(-3 * time.Hour)
	if _, _, err := st.UpsertArticle(ctx, models.Article{
		FeedID: f.ID, GUID: "a1", Title: "T", ContentHash: "h1",
		FetchedAt: fetched.Unix(), PublishedAt: fetched.Unix(),
		SummaryModel: "noop", Summary: "Lead.",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDigest(ctx, models.UserDigest{
		UserID: u.ID, Enabled: true, HourUTC: now.Add(-time.Hour).Hour(),
		ViewKind: "feed", ViewValue: strconv.FormatInt(f.ID, 10),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkDigestSent(ctx, u.ID, lastSent.Unix()); err != nil {
		t.Fatal(err)
	}

	// An SMTP relay that accepts and immediately hangs up: every send fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var dials atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			dials.Add(1)
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	smtpCfg := store.SMTPSettings{Host: "127.0.0.1", Port: port, From: "ember@example.com"}
	sender := &digest.Sender{Store: st}
	retry := map[int64]time.Time{}

	lastSentAt := func() int64 {
		t.Helper()
		d, err := st.GetDigest(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return d.LastSentAt
	}
	waitDials := func(want int32) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for dials.Load() < want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := dials.Load(); got != want {
			t.Fatalf("SMTP dials = %d, want %d", got, want)
		}
	}

	tickDigest(ctx, st, sender, smtpCfg, lg, now, retry)
	waitDials(1)
	if got := lastSentAt(); got != lastSent.Unix() {
		t.Fatalf("failed send moved last_sent_at to %d; want it left at %d", got, lastSent.Unix())
	}

	// The next tick is inside the back-off: no new attempt.
	tickDigest(ctx, st, sender, smtpCfg, lg, now.Add(5*time.Minute), retry)
	time.Sleep(50 * time.Millisecond)
	waitDials(1)

	// After the back-off it is attempted again.
	tickDigest(ctx, st, sender, smtpCfg, lg, now.Add(digestRetryAfter), retry)
	waitDials(2)
	if got := lastSentAt(); got != lastSent.Unix() {
		t.Fatalf("second failed send moved last_sent_at to %d", got)
	}
}
