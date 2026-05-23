package store

import (
	"context"
	"errors"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func TestCategories_CRUD(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u1", PasswordHash: "h"})

	c, err := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "Tech", Color: "#f60"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	got, err := s.GetCategory(ctx, u.ID, c.ID)
	if err != nil || got.Name != "Tech" || got.Color != "#f60" {
		t.Fatalf("GetCategory: %v %+v", err, got)
	}

	newName := "Tech News"
	if err := s.UpdateCategory(ctx, u.ID, c.ID, UpdateCategoryPatch{Name: &newName}); err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	got, _ = s.GetCategory(ctx, u.ID, c.ID)
	if got.Name != "Tech News" {
		t.Errorf("name = %q", got.Name)
	}

	list, _ := s.ListCategories(ctx, u.ID)
	if len(list) != 1 {
		t.Errorf("list = %d", len(list))
	}

	if err := s.DeleteCategory(ctx, u.ID, c.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	if _, err := s.GetCategory(ctx, u.ID, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCategories_CrossUserIsolation(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	a, _ := s.CreateUser(ctx, models.User{Username: "a", PasswordHash: "h"})
	b, _ := s.CreateUser(ctx, models.User{Username: "b", PasswordHash: "h"})
	ac, _ := s.CreateCategory(ctx, models.Category{UserID: a.ID, Name: "A's"})

	// B cannot see A's category.
	if _, err := s.GetCategory(ctx, b.ID, ac.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("B should not see A's category, got %v", err)
	}
	// B cannot delete it.
	if err := s.DeleteCategory(ctx, b.ID, ac.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("B should not delete A's category, got %v", err)
	}
	// B's list is empty.
	list, _ := s.ListCategories(ctx, b.ID)
	if len(list) != 0 {
		t.Errorf("B's list = %d, want 0", len(list))
	}
}

func TestCategories_DuplicateNameConflict(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	_, err := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "Tech"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "Tech"})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestCategories_DeleteNullsSubscription(t *testing.T) {
	s := NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	c, _ := s.CreateCategory(ctx, models.Category{UserID: u.ID, Name: "Tech"})
	f, _ := s.UpsertFeed(ctx, models.Feed{URL: "https://x.test/feed", Title: "X"})
	sub, _ := s.Subscribe(ctx, models.Subscription{
		UserID: u.ID, FeedID: f.ID, CategoryID: &c.ID,
	})
	if err := s.DeleteCategory(ctx, u.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSubscriptionByID(ctx, u.ID, sub.ID)
	if got.CategoryID != nil {
		t.Errorf("subscription category should be nil after delete, got %v", *got.CategoryID)
	}
}
