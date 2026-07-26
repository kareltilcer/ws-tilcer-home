package httpx

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SPAHandler serves the built single-page app from dir. A request that resolves
// to an existing file (the JS/CSS/font bundles Vite emits) is served directly;
// any other path falls back to index.html so the client-side router can resolve
// it — that is what makes deep links like /ukoly work on a fresh page load.
//
// It is only reached for paths that matched no server route. The router keeps
// /api/** and /ws out of this fallback (they get a JSON 404 instead) so a
// mistyped endpoint fails loudly rather than silently returning the SPA shell.
func SPAHandler(dir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(dir))
	indexPath := filepath.Join(dir, "index.html")

	return func(w http.ResponseWriter, r *http.Request) {
		// Resolve the request path under dir. path.Clean collapses any "../" so a
		// crafted path cannot escape the static root (http.FileServer guards this
		// too, but we stat first to decide file-vs-fallback).
		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		full := filepath.Join(dir, filepath.FromSlash(clean))

		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			// Vite emits content-hashed bundles under /assets/, so they are safe
			// to cache forever; a new deploy changes the hash (and index.html).
			if strings.HasPrefix(clean, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// Client-side route or unknown path: serve the SPA shell. index.html must
		// not be cached, so a redeploy is picked up on the next navigation.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, indexPath)
	}
}
