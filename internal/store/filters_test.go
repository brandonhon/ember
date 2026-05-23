package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func TestFilters_CRUD(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})

	f, err := s.CreateFilter(ctx, models.Filter{
		UserID: u.ID, Name: "Hide crypto",
		MatchJSON: `{"field":"title","op":"contains","value":"crypto"}`,
		Action:    "mark_read", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetFilter(ctx, u.ID, f.ID)
	if err != nil || got.Action != "mark_read" || !got.Enabled {
		t.Fatalf("get: %v %+v", err, got)
	}

	disabled := false
	if err := s.UpdateFilter(ctx, u.ID, f.ID, UpdateFilterPatch{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFilter(ctx, u.ID, f.ID)
	if got.Enabled {
		t.Error("filter should be disabled")
	}

	list, _ := s.ListFilters(ctx, u.ID)
	if len(list) != 1 {
		t.Errorf("list = %d", len(list))
	}
	active, _ := s.ListActiveFilters(ctx, u.ID)
	if len(active) != 0 {
		t.Errorf("active = %d, want 0 after disable", len(active))
	}

	if err := s.DeleteFilter(ctx, u.ID, f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetFilter(ctx, u.ID, f.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected not found after delete, got %v", err)
	}
}

func TestFilters_CrossUserIsolation(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, models.User{Username: "a", PasswordHash: "h"})
	b, _ := s.CreateUser(ctx, models.User{Username: "b", PasswordHash: "h"})

	af, _ := s.CreateFilter(ctx, models.Filter{
		UserID: a.ID, Name: "X", MatchJSON: "{}", Action: "mark_read", Enabled: true,
	})

	// B cannot see A's filter.
	if _, err := s.GetFilter(ctx, b.ID, af.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user get should be ErrNotFound, got %v", err)
	}
	list, _ := s.ListFilters(ctx, b.ID)
	if len(list) != 0 {
		t.Errorf("B's filter list should be empty, got %d", len(list))
	}
}
