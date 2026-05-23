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
		// Long-cache hashed assets in /assets/.
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
		// doesn't exist, serve index.html.
		if !strings.HasPrefix(r.URL.Path, "/assets/") && !strings.Contains(r.URL.Path, ".") {
			if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
