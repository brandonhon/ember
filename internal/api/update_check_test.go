package api

import (
	"net/http"
	"testing"

	"github.com/brandonhon/ember/internal/updatecheck"
)

// stubUpdateStatus is a canned UpdateStatus for exercising the /api/me gating
// without hitting GitHub.
type stubUpdateStatus struct {
	res updatecheck.Result
	ok  bool
}

func (s stubUpdateStatus) Latest() (updatecheck.Result, bool) { return s.res, s.ok }

// meWithUpdate is the subset of the /api/me envelope we assert on.
type meWithUpdate struct {
	Data struct {
		Update *updatecheck.Result `json:"update"`
	} `json:"data"`
}

func TestMe_UpdateHint_AdminOnly(t *testing.T) {
	stub := stubUpdateStatus{
		res: updatecheck.Result{Current: "v0.9.4", Latest: "v0.9.5", Available: true, URL: "https://example.test/v0.9.5"},
		ok:  true,
	}
	h := newHarnessWith(t, func(d *Dependencies) { d.UpdateChecker = stub })
	h.seedUser(t, "root", "password123", true)
	h.seedUser(t, "reader", "password123", false)

	// Admin sees the update object.
	var adminMe meWithUpdate
	admin := h.login(t, "root", "password123")
	if code := get(t, admin, h.srv.URL+"/api/me", &adminMe); code != http.StatusOK {
		t.Fatalf("admin /api/me: %d", code)
	}
	if adminMe.Data.Update == nil {
		t.Fatal("admin should see the update object")
	}
	if !adminMe.Data.Update.Available || adminMe.Data.Update.Latest != "v0.9.5" {
		t.Errorf("unexpected update payload: %+v", adminMe.Data.Update)
	}

	// Non-admin must never see it.
	var readerMe meWithUpdate
	reader := h.login(t, "reader", "password123")
	if code := get(t, reader, h.srv.URL+"/api/me", &readerMe); code != http.StatusOK {
		t.Fatalf("reader /api/me: %d", code)
	}
	if readerMe.Data.Update != nil {
		t.Errorf("non-admin must not receive the update object; got %+v", readerMe.Data.Update)
	}
}

func TestMe_UpdateHint_OmittedWhenNoChecker(t *testing.T) {
	// Default harness wires no UpdateChecker → the field is omitted even for an
	// admin.
	h := newHarness(t)
	h.seedUser(t, "root", "password123", true)
	var me meWithUpdate
	admin := h.login(t, "root", "password123")
	if code := get(t, admin, h.srv.URL+"/api/me", &me); code != http.StatusOK {
		t.Fatalf("/api/me: %d", code)
	}
	if me.Data.Update != nil {
		t.Errorf("expected no update object without a checker; got %+v", me.Data.Update)
	}
}
