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
	b := branding{Name: "Ember", PageTitle: "Ember", FaviconURL: "/icon.svg"}
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
	// The favicon URL is handed straight to the SPA as a <link rel="icon"> href.
	// Only allow an empty value (clears the override), a same-origin absolute
	// path, or an explicit https URL — never a javascript:/data: scheme that the
	// DOM would treat as active content.
	if req.FaviconURL != nil {
		v := strings.TrimSpace(*req.FaviconURL)
		if v != "" && !strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "https://") {
			writeError(w, http.StatusBadRequest, "bad_request", "favicon_url must be empty, a /path, or an https:// URL")
			return
		}
	}
	apply := func(key string, val *string) error {
		if val == nil {
			return nil
		}
		return d.Store.PutAppSetting(r.Context(), key, strings.TrimSpace(*val))
	}
	if err := apply(keyBrandName, req.Name); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := apply(keyBrandTitle, req.PageTitle); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := apply(keyBrandFavicon, req.FaviconURL); err != nil {
		internalError(w, "internal", err)
		return
	}
	writeData(w, http.StatusOK, d.currentBranding(r), nil)
}
