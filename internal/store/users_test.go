package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func TestUsers_CRUD(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "hash1",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("expected id assigned")
	}
	if u.SettingsJSON != "{}" {
		t.Errorf("default SettingsJSON = %q", u.SettingsJSON)
	}

	got, err := s.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Username != "alice" || got.Email != "alice@example.com" {
		t.Errorf("got %+v", got)
	}

	byName, err := s.GetUserByUsername(ctx, "alice")
	if err != nil || byName.ID != u.ID {
		t.Fatalf("GetUserByUsername: %v %+v", err, byName)
	}

	// Update password + admin flag.
	newHash := "hash2"
	admin := true
	if err := s.UpdateUser(ctx, u.ID, UpdateUserPatch{
		PasswordHash: &newHash, IsAdmin: &admin,
	}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, _ = s.GetUser(ctx, u.ID)
	if got.PasswordHash != "hash2" || !got.IsAdmin {
		t.Errorf("update not applied: %+v", got)
	}

	// List.
	list, err := s.ListUsers(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListUsers: %v len=%d", err, len(list))
	}

	// Delete.
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := s.GetUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUsers_DuplicateUsernameConflict(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	_, err := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestDeleteUser_DropsOrphanFeeds(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()

	// alice subscribes to "solo" (no one else) and "shared" (also bob).
	// bob subscribes only to "shared".
	aliceID, _ := seedUserAndFeed(t, s, "alice")
	bobID, sharedFeedID := seedUserAndFeed(t, s, "bob")

	soloFeed, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://solo.test/feed", Title: "solo"})
	if _, err := s.Subscribe(ctx, models.Subscription{UserID: aliceID, FeedID: soloFeed.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Subscribe(ctx, models.Subscription{UserID: aliceID, FeedID: sharedFeedID}); err != nil {
		t.Fatal(err)
	}

	// Drop alice. Expect:
	// - "solo" feed gone (alice was the only subscriber).
	// - "shared" feed retained (bob still subscribes).
	// - alice's own seeded feed gone (only alice subscribed).
	if err := s.DeleteUser(ctx, aliceID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := s.GetFeed(ctx, soloFeed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("solo feed should be gone, got err=%v", err)
	}
	if _, err := s.GetFeed(ctx, sharedFeedID); err != nil {
		t.Errorf("shared feed should remain (bob subscribes), got err=%v", err)
	}
	// bob is still here.
	if _, err := s.GetUser(ctx, bobID); err != nil {
		t.Errorf("bob should be untouched: %v", err)
	}
}

func TestUsers_CountAndNotFound(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	n, err := s.CountUsers(ctx)
	if err != nil || n != 0 {
		t.Fatalf("CountUsers initial: %d %v", n, err)
	}
	_, _ = s.CreateUser(ctx, models.User{Username: "a", PasswordHash: "h"})
	_, _ = s.CreateUser(ctx, models.User{Username: "b", PasswordHash: "h"})
	n, _ = s.CountUsers(ctx)
	if n != 2 {
		t.Errorf("CountUsers = %d, want 2", n)
	}
	if _, err := s.GetUser(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing user should be ErrNotFound, got %v", err)
	}
	if err := s.DeleteUser(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing should be ErrNotFound, got %v", err)
	}
	if err := s.UpdateUser(ctx, 9999, UpdateUserPatch{}); err != nil {
		t.Errorf("empty patch on missing user should be noop, got %v", err)
	}
}
