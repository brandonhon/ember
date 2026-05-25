package api

import (
	"net/http"
	"strings"
)

// branding is the public-facing app identity. Stored as discrete keys in
// app_settings; the SPA reads it once at boot to set the document title and
// favicon. Defaults match the stock Ember build.
type branding struct {
	Name       string `json:"name"`
	PageTitle  string `json:"page_title"`
	FaviconURL string `json:"favicon_url"`
}

const (
	keyBrandName    = "branding_name"
	keyBrandTitle   = "branding_page_title"
	keyBrandFavicon = "branding_favicon_url"
)

func (d *Dependencies) currentBranding(r *http.Request) branding {
	b := branding{Name: "Ember", PageTitle: "Ember Reader", FaviconURL: "/favicon.svg"}
	if v, _ := d.Store.GetAppSetting(r.Context(), keyBrandName); v != "" {
		b.Name = v
	}
	if v, _ := d.Store.GetAppSetting(r.Context(), keyBrandTitle); v != "" {
		b.PageTitle = v
	}
	if v, _ := d.Store.GetAppSetting(r.Context(), keyBrandFavicon); v != "" {
		b.FaviconURL = v
	}
	return b
}

// handleGetBranding is public: the SPA needs to know the app's identity
// before the user logs in so the login page shows the right name.
func (d *Dependencies) handleGetBranding(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, d.currentBranding(r), nil)
}

type setBrandingReq struct {
	Name       *string `json:"name,omitempty"`
	PageTitle  *string `json:"page_title,omitempty"`
	FaviconURL *string `json:"favicon_url,omitempty"`
}

// handleSetBranding is admin-only. Each field is optional; only provided keys
// are written. An empty string clears the override (falls back to default).
func (d *Dependencies) handleSetBranding(w http.ResponseWriter, r *http.Request) {
	var req setBrandingReq
	if !decodeJSON(w, r, &req) {
		return
	}
	apply := func(key string, val *string) error {
		if val == nil {
			return nil
		}
		return d.Store.PutAppSetting(r.Context(), key, strings.TrimSpace(*val))
	}
	if err := apply(keyBrandName, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := apply(keyBrandTitle, req.PageTitle); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := apply(keyBrandFavicon, req.FaviconURL); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeData(w, http.StatusOK, d.currentBranding(r), nil)
}
