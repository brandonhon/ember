package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func TestCreatePushSubscription_OwnershipGuard(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	alice, _ := s.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "h"})
	bob, _ := s.CreateUser(ctx, models.User{Username: "bob", PasswordHash: "h"})

	const ep = "https://push.example/ep/abc"

	id1, err := s.CreatePushSubscription(ctx, alice.ID, ep, "p1", "a1", "ua1")
	if err != nil || id1 == 0 {
		t.Fatalf("alice create: id=%d err=%v", id1, err)
	}

	// Alice re-subscribes the same endpoint (new keys) → same row id, updated.
	id2, err := s.CreatePushSubscription(ctx, alice.ID, ep, "p2", "a2", "ua2")
	if err != nil || id2 != id1 {
		t.Fatalf("alice re-subscribe: id=%d (want %d) err=%v", id2, id1, err)
	}

	// Bob submits Alice's endpoint → must be rejected, not hijacked.
	if _, err := s.CreatePushSubscription(ctx, bob.ID, ep, "pX", "aX", "uaX"); !errors.Is(err, ErrConflict) {
		t.Fatalf("bob hijack attempt: want ErrConflict, got %v", err)
	}

	// Row still belongs to Alice; Bob has none.
	asubs, _ := s.ListSubscriptionsForUser(ctx, alice.ID)
	if len(asubs) != 1 || asubs[0].P256dh != "p2" {
		t.Errorf("alice subs after hijack attempt: %+v", asubs)
	}
	bsubs, _ := s.ListSubscriptionsForUser(ctx, bob.ID)
	if len(bsubs) != 0 {
		t.Errorf("bob should have no subs, got %+v", bsubs)
	}
}
