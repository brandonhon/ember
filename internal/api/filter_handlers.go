package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/filters"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// maxFiltersPerUser bounds how many filters one account can create; see
// handleCreateFilter.
const maxFiltersPerUser = 200

// publicFilterErr surfaces a filter-validation error to the client without the
// internal "filters:" package prefix. These messages only ever echo the user's
// own rule (bad field/op/regex/action), so the detail is safe and useful — we
// just don't leak the package name across the API boundary.
func publicFilterErr(err error) string {
	return strings.TrimPrefix(err.Error(), "filters: ")
}

type filterReq struct {
	Name        string `json:"name"`
	MatchJSON   string `json:"match_json"`
	Action      string `json:"action"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Priority    *int   `json:"priority,omitempty"`
	ActionValue string `json:"action_value,omitempty"`
}

func (d *Dependencies) handleListFilters(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	fs, err := d.Store.ListFilters(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, fs, nil)
}

func (d *Dependencies) handleCreateFilter(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req filterReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	// Validate match + action up front so bad data never lands in the DB.
	if _, err := filters.ParseMatch(req.MatchJSON); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", publicFilterErr(err))
		return
	}
	if err := filters.ValidateActionWithValue(req.Action, req.ActionValue); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", publicFilterErr(err))
		return
	}
	// Cap filters per user: each filter's regex is compiled and cached for the
	// process lifetime, so an unbounded count is a memory-DoS vector. 200 is
	// far beyond any realistic use.
	existing, err := d.Store.ListFilters(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	if len(existing) >= maxFiltersPerUser {
		writeError(w, http.StatusBadRequest, "filter_limit",
			"filter limit reached (max 200 per user)")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	priority := 100
	if req.Priority != nil {
		priority = *req.Priority
	}
	f, err := d.Store.CreateFilter(r.Context(), models.Filter{
		UserID: u.ID, Name: req.Name, MatchJSON: req.MatchJSON,
		Action: req.Action, Enabled: enabled,
		Priority: priority, ActionValue: req.ActionValue,
	})
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusCreated, f, nil)
}

func (d *Dependencies) handleUpdateFilter(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	var req filterReq
	if !decodeJSON(w, r, &req) {
		return
	}
	patch := store.UpdateFilterPatch{}
	if req.Name != "" {
		patch.Name = &req.Name
	}
	if req.MatchJSON != "" {
		if _, err := filters.ParseMatch(req.MatchJSON); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", publicFilterErr(err))
			return
		}
		patch.MatchJSON = &req.MatchJSON
	}
	if req.Action != "" {
		if err := filters.ValidateActionWithValue(req.Action, req.ActionValue); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", publicFilterErr(err))
			return
		}
		patch.Action = &req.Action
	}
	if req.Enabled != nil {
		patch.Enabled = req.Enabled
	}
	if req.Priority != nil {
		patch.Priority = req.Priority
	}
	// Treat any non-empty action_value as an intentional update; an empty
	// string means "no change" rather than "clear it" (avoids accidentally
	// wiping the payload on a PATCH that only touches name / enabled).
	if req.ActionValue != "" {
		// When the action itself isn't part of this PATCH, validate the new
		// value against the stored action so e.g. a board filter can't be
		// given a non-numeric value (or a tag filter a board id).
		if req.Action == "" {
			existing, err := d.Store.GetFilter(r.Context(), u.ID, id)
			if mapStoreError(w, err) {
				return
			}
			if err := filters.ValidateActionWithValue(existing.Action, req.ActionValue); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", publicFilterErr(err))
				return
			}
		}
		patch.ActionValue = &req.ActionValue
	}
	if mapStoreError(w, d.Store.UpdateFilter(r.Context(), u.ID, id, patch)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

// handlePreviewFilter returns the count of articles over the last
// `since_days` (default 7) that would have matched the supplied
// match_json. Used by the rule-builder UI to give an at-a-glance
// "would have hit N items" before saving.
func (d *Dependencies) handlePreviewFilter(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req struct {
		MatchJSON string `json:"match_json"`
		SinceDays int    `json:"since_days,omitempty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := filters.ParseMatch(req.MatchJSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", publicFilterErr(err))
		return
	}
	count, err := d.Store.PreviewFilter(r.Context(), u.ID, m, req.SinceDays)
	if mapStoreError(w, err) {
		return
	}
	writeData(w, http.StatusOK, map[string]int{"count": count}, nil)
}

func (d *Dependencies) handleDeleteFilter(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, ok := paramInt(w, r, "id")
	if !ok {
		return
	}
	if mapStoreError(w, d.Store.DeleteFilter(r.Context(), u.ID, id)) {
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"ok": true}, nil)
}

// filterExport is the portable shape of a filter — no instance-specific id,
// owner, or timestamp — used for backup/restore across instances.
type filterExport struct {
	Name        string `json:"name"`
	MatchJSON   string `json:"match_json"`
	Action      string `json:"action"`
	ActionValue string `json:"action_value,omitempty"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
}

type filtersBundle struct {
	Version int            `json:"version"`
	Filters []filterExport `json:"filters"`
}

// handleExportFilters returns the user's filters as a downloadable JSON backup.
func (d *Dependencies) handleExportFilters(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	fs, err := d.Store.ListFilters(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	bundle := filtersBundle{Version: 1, Filters: make([]filterExport, 0, len(fs))}
	for _, f := range fs {
		bundle.Filters = append(bundle.Filters, filterExport{
			Name: f.Name, MatchJSON: f.MatchJSON, Action: f.Action,
			ActionValue: f.ActionValue, Enabled: f.Enabled, Priority: f.Priority,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="ember-filters.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(bundle)
}

// handleImportFilters creates filters from an uploaded backup. Each entry is
// validated like a manual create; invalid entries and any beyond the per-user
// cap are skipped (reported in the response) rather than failing the import.
func (d *Dependencies) handleImportFilters(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var bundle filtersBundle
	if !decodeJSON(w, r, &bundle) {
		return
	}
	existing, err := d.Store.ListFilters(r.Context(), u.ID)
	if mapStoreError(w, err) {
		return
	}
	room := maxFiltersPerUser - len(existing)
	imported, skipped := 0, 0
	for _, fe := range bundle.Filters {
		if imported >= room || fe.Name == "" {
			skipped++
			continue
		}
		if _, perr := filters.ParseMatch(fe.MatchJSON); perr != nil {
			skipped++
			continue
		}
		if verr := filters.ValidateActionWithValue(fe.Action, fe.ActionValue); verr != nil {
			skipped++
			continue
		}
		priority := fe.Priority
		if priority <= 0 {
			priority = 100
		}
		if _, cerr := d.Store.CreateFilter(r.Context(), models.Filter{
			UserID: u.ID, Name: fe.Name, MatchJSON: fe.MatchJSON, Action: fe.Action,
			Enabled: fe.Enabled, Priority: priority, ActionValue: fe.ActionValue,
		}); cerr != nil {
			skipped++
			continue
		}
		imported++
	}
	writeData(w, http.StatusOK, map[string]int{"imported": imported, "skipped": skipped}, nil)
}
