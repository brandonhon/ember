package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

// POST /api/feeds accepts a folder for the new subscription, but the folder has
// to be a real category owned by the caller: the subscriptions FK only proves
// the row exists, not whose it is.
func TestAddFeed_CategoryOwnershipAndValidation(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	h.seedUser(t, "bob", "p", false)
	cA := h.login(t, "alice", "p")
	cB := h.login(t, "bob", "p")

	var cat struct {
		Data models.Category `json:"data"`
	}
	if code := post(t, cA, h.srv.URL+"/api/categories",
		map[string]string{"name": "Tech"}, &cat); code != http.StatusCreated {
		t.Fatalf("create category = %d", code)
	}

	var added struct {
		Data struct {
			Subscription models.Subscription `json:"subscription"`
		} `json:"data"`
	}
	// Alice's own folder: the subscription lands in it.
	if code := post(t, cA, h.srv.URL+"/api/feeds",
		map[string]any{"url": "https://a.test/feed", "category_id": cat.Data.ID},
		&added); code != http.StatusCreated {
		t.Fatalf("add feed with category = %d", code)
	}
	if added.Data.Subscription.CategoryID == nil || *added.Data.Subscription.CategoryID != cat.Data.ID {
		t.Errorf("subscription category = %v, want %d", added.Data.Subscription.CategoryID, cat.Data.ID)
	}

	// Bob cannot file his subscription under Alice's folder.
	if code := post(t, cB, h.srv.URL+"/api/feeds",
		map[string]any{"url": "https://b.test/feed", "category_id": cat.Data.ID}, nil); code != http.StatusNotFound {
		t.Errorf("cross-user category on add = %d, want 404", code)
	}

	// 0 is not a category row — it would reach SQLite as an FK violation and
	// come back as a 500. "No folder" is an omitted category_id.
	if code := post(t, cA, h.srv.URL+"/api/feeds",
		map[string]any{"url": "https://c.test/feed", "category_id": 0}, nil); code != http.StatusBadRequest {
		t.Errorf("category_id 0 on add = %d, want 400", code)
	}

	// Omitting it subscribes with no folder.
	var uncat struct {
		Data struct {
			Subscription models.Subscription `json:"subscription"`
		} `json:"data"`
	}
	if code := post(t, cA, h.srv.URL+"/api/feeds",
		map[string]any{"url": "https://d.test/feed"}, &uncat); code != http.StatusCreated {
		t.Fatalf("add feed without category = %d", code)
	}
	if uncat.Data.Subscription.CategoryID != nil {
		t.Errorf("subscription category = %v, want nil", uncat.Data.Subscription.CategoryID)
	}
}

// PATCH /api/feeds/{id} with category_id 0 used to hit the subscriptions FK and
// return 500. Clearing a folder is clear_category.
func TestUpdateFeed_CategoryZeroIsBadRequest(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cA := h.login(t, "alice", "p")

	var cat struct {
		Data models.Category `json:"data"`
	}
	post(t, cA, h.srv.URL+"/api/categories", map[string]string{"name": "Tech"}, &cat)

	var added struct {
		Data struct {
			Subscription models.Subscription `json:"subscription"`
		} `json:"data"`
	}
	if code := post(t, cA, h.srv.URL+"/api/feeds",
		map[string]any{"url": "https://a.test/feed", "category_id": cat.Data.ID},
		&added); code != http.StatusCreated {
		t.Fatalf("add feed = %d", code)
	}
	subURL := fmt.Sprintf("%s/api/feeds/%d", h.srv.URL, added.Data.Subscription.ID)

	if code := patch(t, cA, subURL, map[string]any{"category_id": 0}, nil); code != http.StatusBadRequest {
		t.Errorf("patch category_id 0 = %d, want 400", code)
	}

	// clear_category is the supported way, and it actually clears.
	if code := patch(t, cA, subURL, map[string]any{"clear_category": true}, nil); code != http.StatusOK {
		t.Errorf("patch clear_category = %d, want 200", code)
	}
	var list struct {
		Data []models.FeedWithCounts `json:"data"`
	}
	get(t, cA, h.srv.URL+"/api/feeds", &list)
	if len(list.Data) != 1 {
		t.Fatalf("feed list len = %d", len(list.Data))
	}
	if list.Data[0].CategoryID != nil {
		t.Errorf("category after clear = %v, want nil", list.Data[0].CategoryID)
	}
}
