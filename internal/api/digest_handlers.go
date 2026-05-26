package api

import (
	"net/http"

	"github.com/brandonhon/ember/internal/auth"
	"github.com/brandonhon/ember/internal/models"
)

type setDigestReq struct {
	Enabled       bool   `json:"enabled"`
	ViewKind      string `json:"view_kind"`
	ViewValue     string `json:"view_value"`
	HourUTC       int    `json:"hour_utc"`
	MinuteUTC     int    `json:"minute_utc"`
	EmailOverride string `json:"email_override"`
}

func (d *Dependencies) handleGetDigest(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	cfg, err := d.Store.GetDigest(r.Context(), u.ID)
	if err != nil {
		internalError(w, "digest/get", err)
		return
	}
	writeData(w, http.StatusOK, cfg, nil)
}

func (d *Dependencies) handleSetDigest(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var req setDigestReq
	if !decodeJSON(w, r, &req) {
		return
	}
	// Clamp hour/minute. View defaults to smart/fresh if empty.
	if req.HourUTC < 0 || req.HourUTC > 23 {
		writeError(w, http.StatusBadRequest, "bad_request", "hour_utc must be 0-23")
		return
	}
	if req.MinuteUTC < 0 || req.MinuteUTC > 59 {
		writeError(w, http.StatusBadRequest, "bad_request", "minute_utc must be 0-59")
		return
	}
	if req.ViewKind == "" {
		req.ViewKind = "smart"
		req.ViewValue = "fresh"
	}
	switch req.ViewKind {
	case "smart", "feed", "category", "board":
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "view_kind must be smart|feed|category|board")
		return
	}
	d2 := models.UserDigest{
		UserID:        u.ID,
		Enabled:       req.Enabled,
		ViewKind:      req.ViewKind,
		ViewValue:     req.ViewValue,
		HourUTC:       req.HourUTC,
		MinuteUTC:     req.MinuteUTC,
		EmailOverride: req.EmailOverride,
	}
	if err := d.Store.UpsertDigest(r.Context(), d2); err != nil {
		internalError(w, "digest/set", err)
		return
	}
	writeData(w, http.StatusOK, d2, nil)
}
