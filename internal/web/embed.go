// Package web serves the built Svelte SPA from an embedded FS.
//
// The build pipeline copies web/dist into internal/web/dist before `go build`
// so the binary embeds the latest assets. SPA history-mode fallback: unknown
// paths return index.html.
package web

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded SPA with history
// fallback. Returns an error if the embedded dist directory is empty (e.g.
// the SPA was not built before linking the binary).
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	// Confirm index.html exists; if not, the embed yielded an empty dir.
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, errors.New("web: dist/index.html not embedded")
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Long-cache hashed assets in /assets/ — Vite gives them content-
		// hashed names so the URL itself changes on every build, making
		// immutable caching safe and aggressive.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		// MIME overrides Go's default mime.TypeByExtension lookup, which
		// doesn't know about .webmanifest.
		switch {
		case strings.HasSuffix(r.URL.Path, ".webmanifest"):
			w.Header().Set("Content-Type", "application/manifest+json")
		case strings.HasSuffix(r.URL.Path, "/sw.js"):
			// Service workers must be served without aggressive caching so
			// updates roll out on next page load.
			w.Header().Set("Cache-Control", "no-cache")
		}
		// SPA history fallback: if the path has no extension and the file
		// doesn't exist, serve index.html. Detect the shell case so we
		// can also tag it no-cache below.
		shell := false
		if !strings.HasPrefix(r.URL.Path, "/assets/") && !strings.Contains(r.URL.Path, ".") {
			if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
				r.URL.Path = "/"
			}
			shell = true
		}
		// The shell (index.html and SPA history-mode fallbacks) must never
		// be heuristic-cached by the browser. Firefox is especially eager
		// to cache HTML for up to 24h via heuristic when no explicit cache
		// header is set, which makes shell changes look like nothing
		// happened from the user's perspective. no-cache forces a
		// revalidation on every navigation.
		if shell || r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, "/index.html") {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
