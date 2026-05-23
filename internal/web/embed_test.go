package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_ServesIndex(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	body, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(body), "Ember") {
		t.Errorf("body missing Ember: %q", string(body))
	}
}

func TestHandler_SPAFallback(t *testing.T) {
	h, _ := Handler()
	// Random nested route with no extension should fall back to index.html.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/some/route", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("fallback status = %d", w.Code)
	}
}
