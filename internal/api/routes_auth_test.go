package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// publicRoutes are the only paths that may be reached without a session. Each
// one is public for a stated reason; everything else under /api must 401.
var publicRoutes = map[string]string{
	"/healthz":                 "liveness probe scraped by the reverse proxy",
	"/readyz":                  "readiness probe; pings the DB but returns no data",
	"/fever":                   "Fever shim, authenticated by its own per-user api_key",
	"/api/auth/login":          "issues the session in the first place",
	"/api/auth/passkey/begin":  "passkey login ceremony, pre-session",
	"/api/auth/passkey/finish": "passkey login ceremony, pre-session",
	"/api/auth/passkey/exists": "boolean probe driving the login page's passkey button",
}

// Every route the router exposes must either be on the public allowlist or
// reject an unauthenticated caller. This is the backstop for the route table:
// a new endpoint registered without RequireAuth fails here rather than shipping
// as an anonymous read of someone's articles.
func TestRoutes_UnauthenticatedAccessIsRejected(t *testing.T) {
	h := newHarness(t)
	// No login — a bare client with a cookie jar so CSRF issuance behaves
	// normally, but no session cookie.
	jar, _ := newJar()
	cl := h.newClient(jar)

	router, ok := NewRouter(h.dep).(chi.Routes)
	if !ok {
		t.Fatal("router does not expose chi.Routes")
	}

	checked := 0
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, public := publicRoutes[route]; public {
			return nil
		}
		// Substitute a concrete value for path params so the request routes.
		path := route
		for _, param := range []string{"{id}", "{articleId}", "{slug}", "{name}"} {
			path = strings.ReplaceAll(path, param, "1")
		}
		if strings.ContainsAny(path, "{*") {
			t.Errorf("route %s has an unsubstituted param — add it to the test or the allowlist", route)
			return nil
		}
		checked++

		req, reqErr := http.NewRequest(method, h.srv.URL+path, strings.NewReader("{}"))
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		echoCSRF(cl, h.srv.URL+path, req)
		resp, doErr := cl.Do(req)
		if doErr != nil {
			t.Fatal(doErr)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s (unauthenticated) = %d, want 401", method, path, resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	// Guard against the walk silently matching nothing (e.g. a chi API change).
	// The router registers ~107 routes, 8 of which are public.
	if checked < 90 {
		t.Errorf("only %d guarded routes walked, expected the full API surface", checked)
	}
}

// adminRoutes are the operator-only endpoints — server-wide configuration,
// destructive maintenance, and anything that spends resources on every user's
// behalf. A signed-in reader must get 403, not just 401-when-anonymous.
var adminRoutes = []struct{ method, path string }{
	{http.MethodGet, "/metrics"},
	{http.MethodPost, "/api/admin/branding"},
	{http.MethodGet, "/api/admin/db"},
	{http.MethodPost, "/api/admin/db/backup"},
	{http.MethodDelete, "/api/admin/db/backups/x.db"},
	{http.MethodPost, "/api/admin/db/opml-export"},
	{http.MethodDelete, "/api/admin/db/exports/x.opml"},
	{http.MethodPost, "/api/admin/db/cleanup"},
	{http.MethodPost, "/api/admin/db/schedule"},
	{http.MethodGet, "/api/admin/llm"},
	{http.MethodPost, "/api/admin/llm/model"},
	{http.MethodPost, "/api/admin/llm/pull"},
	{http.MethodPost, "/api/admin/llm/delete"},
	{http.MethodPost, "/api/admin/llm/options"},
	{http.MethodGet, "/api/admin/session"},
	{http.MethodPost, "/api/admin/session/ttl"},
	{http.MethodGet, "/api/admin/settings"},
	{http.MethodPatch, "/api/admin/settings"},
	{http.MethodPost, "/api/admin/settings/email-test"},
	{http.MethodPost, "/api/feeds/resummarize-all"},
	{http.MethodPost, "/api/users"},
	{http.MethodPatch, "/api/users/1"},
	{http.MethodDelete, "/api/users/1"},
}

// Companion to the 401 walk above: every admin endpoint must also reject a
// signed-in non-admin. Catches an admin route that gets grouped under plain
// RequireAuth, which the 401 test alone would not notice.
func TestRoutes_AdminEndpointsRejectNonAdmin(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "p", false)
	cl := h.login(t, "alice", "p")

	for _, rt := range adminRoutes {
		req, err := http.NewRequest(rt.method, h.srv.URL+rt.path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		echoCSRF(cl, h.srv.URL+rt.path, req)
		resp, err := cl.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as non-admin = %d, want 403", rt.method, rt.path, resp.StatusCode)
		}
	}
}
