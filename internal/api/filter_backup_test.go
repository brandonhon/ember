package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Filters export to a portable JSON bundle and import back (e.g. into another
// account/instance); invalid entries in an imported bundle are skipped, not
// fatal.
func TestFilters_ExportImport(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "pw123456", false)
	h.seedUser(t, "bob", "pw123456", false)
	alice := h.login(t, "alice", "pw123456")
	bob := h.login(t, "bob", "pw123456")

	mk := func(name, match string) {
		if code := post(t, alice, h.srv.URL+"/api/filters", map[string]any{
			"name": name, "match_json": match, "action": "mark_read",
		}, nil); code != http.StatusCreated {
			t.Fatalf("create %q = %d, want 201", name, code)
		}
	}
	mk("hide crypto", `{"field":"title","op":"contains","value":"crypto"}`)
	mk("star nilay", `{"field":"author","op":"equals","value":"Nilay"}`)

	// Export alice's filters (raw body, not the {data:...} envelope).
	resp, err := alice.Get(h.srv.URL + "/api/filters/export")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Version int              `json:"version"`
		Filters []map[string]any `json:"filters"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&bundle)
	resp.Body.Close()
	if len(bundle.Filters) != 2 {
		t.Fatalf("export has %d filters, want 2", len(bundle.Filters))
	}

	// Bob imports a bundle with the two valid filters plus one invalid one.
	bad := map[string]any{"name": "bad", "match_json": "{not json", "action": "mark_read"}
	importBody := map[string]any{"version": 1, "filters": append(bundle.Filters, bad)}
	var ir struct {
		Data struct {
			Imported int `json:"imported"`
			Skipped  int `json:"skipped"`
		} `json:"data"`
	}
	if code := post(t, bob, h.srv.URL+"/api/filters/import", importBody, &ir); code != http.StatusOK {
		t.Fatalf("import = %d, want 200", code)
	}
	if ir.Data.Imported != 2 || ir.Data.Skipped != 1 {
		t.Fatalf("import imported=%d skipped=%d, want 2/1", ir.Data.Imported, ir.Data.Skipped)
	}

	var got struct {
		Data []map[string]any `json:"data"`
	}
	if code := get(t, bob, h.srv.URL+"/api/filters", &got); code != http.StatusOK || len(got.Data) != 2 {
		t.Fatalf("bob filters: code=%d count=%d, want 200/2", code, len(got.Data))
	}
}
